package main

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// IIA Industrial Directory (directoryiia.com): a public, server-rendered PHP
// directory of (mostly Kanpur/Lucknow, UP) companies. The category index lists
// ~159 sub-categories; each sub-category page carries the full company blocks
// INLINE — name, address, phone(s), EMAIL, website — so there is no per-company
// detail page. Discover does all the fetching/parsing and embeds a pre-parsed
// Lead in each ref; Fetch is a no-op lookup. Notable vs delhichamber: emails are
// present, which is the scarce field for the email channel.
const diiaBase = "https://directoryiia.com/"

var diiaSubRe = regexp.MustCompile(`sub_category_listing\.php\?sdftghj=[A-Za-z0-9=]+`)

func discoverDirectoryIIA(ctx context.Context, c *Client) ([]memberRef, error) {
	idx, err := c.get(ctx, diiaBase+"category_listing.php")
	if err != nil {
		return nil, fmt.Errorf("category index: %w", err)
	}
	cats := uniqueStrings(diiaSubRe.FindAllString(idx, -1))
	var refs []memberRef
	seen := map[string]bool{}
	for _, cat := range cats {
		body, err := c.get(ctx, diiaBase+cat)
		if err != nil {
			continue // skip a bad category page, keep going
		}
		for _, l := range parseDIIACompanies(body) {
			id := l.MemID
			if id == "" || seen[id] {
				continue
			}
			seen[id] = true
			refs = append(refs, memberRef{ID: id, Name: l.Company, lead: l})
		}
	}
	return refs, nil
}

func fetchDirectoryIIA(_ context.Context, _ *Client, ref memberRef) (*Lead, error) {
	if ref.lead == nil {
		return nil, fmt.Errorf("no inline lead for %s", ref.ID)
	}
	return ref.lead, nil
}

// parseDIIACompanies parses the company blocks on a sub-category page. Each
// company is a run of <div class="company-detial1"> cells that begins with a
// "Category :" cell; the next unlabelled cell is the name (text before
// " Deals In"), and the following labelled cells carry the contact fields.
func parseDIIACompanies(body string) []*Lead {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(body))
	if err != nil {
		return nil
	}
	var out []*Lead
	var cur *Lead
	flush := func() {
		if cur != nil && cur.Company != "" {
			cur.MemID = cur.Email
			if cur.MemID == "" {
				cur.MemID = normKey(cur.Company)
			}
			out = append(out, cur)
		}
		cur = nil
	}
	doc.Find("div.company-detial1").Each(func(_ int, s *goquery.Selection) {
		txt := clean(s.Text())
		low := strings.ToLower(txt)
		switch {
		case strings.HasPrefix(low, "category :"):
			flush()
			cur = &Lead{Source: "directoryiia.com", BusinessType: afterColon(txt)}
		case cur == nil:
			return
		case cur.Company == "" && strings.Contains(txt, " Deals In"):
			cur.Company = strings.TrimSpace(txt[:strings.Index(txt, " Deals In")])
		case strings.HasPrefix(low, "address :"):
			cur.Address = afterColon(txt)
		case strings.HasPrefix(low, "contact no."):
			cur.Phone = cleanContacts(afterColon(txt))
		case strings.HasPrefix(low, "email id"):
			if v := afterColon(txt); v != "" && v != "-" {
				cur.Email = v
			}
		case strings.HasPrefix(low, "web site"):
			if v := afterColon(txt); v != "" && v != "-" && !strings.Contains(strings.ToLower(v), "directoryiia") {
				cur.Website = v
			}
		case cur.Company == "" && !looksLabelled(low):
			cur.Company = txt // fallback: first unlabelled cell is the name
		}
	})
	flush()
	return out
}

func afterColon(s string) string {
	if i := strings.Index(s, ":"); i >= 0 {
		return strings.TrimSpace(s[i+1:])
	}
	return strings.TrimSpace(s)
}

func looksLabelled(low string) bool {
	for _, p := range []string{"category :", "address :", "contact no.", "email id", "web site"} {
		if strings.HasPrefix(low, p) {
			return true
		}
	}
	return false
}

// cleanContacts collapses "9XXXXXXXXX, , , 0XXX-XXXXXXX,," into "9XXXXXXXXX, 0XXX-XXXXXXX".
func cleanContacts(s string) string {
	var parts []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			parts = append(parts, p)
		}
	}
	return strings.Join(parts, ", ")
}

func uniqueStrings(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}
