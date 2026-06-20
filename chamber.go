package main

import (
	"context"
	"fmt"
	"html"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// Delhi Chamber of Commerce member directory: a public, server-rendered
// directory of Delhi NCR companies. List pages (?ALP=<letter>) pair each
// company name with a member ID; detail pages (?MemID=<id>) carry the
// contact block.
const chamberBase = "http://www.delhichamber.com/"

var (
	memIDRe = regexp.MustCompile(`MemID=(\d+)`)
	tagRe   = regexp.MustCompile(`<[^>]+>`)
	yearRe  = regexp.MustCompile(`\d{4}`)
)

// memberRef is a company discovered on a list page: its ID and (clean) name.
type memberRef struct {
	ID   string
	Name string
}

// discoverMembers walks the requested member-list pages and returns unique
// members in first-seen order, name sourced from the list page (where it sits
// in the <td> directly before the "View Details" link).
func discoverMembers(ctx context.Context, c *Client, letters string) ([]memberRef, error) {
	seen := map[string]bool{}
	var refs []memberRef
	for _, r := range letters {
		letter := strings.ToUpper(string(r))
		url := fmt.Sprintf("%sMembers-List.asp?ALP=%s", chamberBase, letter)
		body, err := c.get(ctx, url)
		if err != nil {
			return nil, fmt.Errorf("list page %s: %w", letter, err)
		}
		doc, err := goquery.NewDocumentFromReader(strings.NewReader(body))
		if err != nil {
			return nil, fmt.Errorf("list page %s: %w", letter, err)
		}
		doc.Find("a[href*='ListingDetails.asp?MemID=']").Each(func(_ int, s *goquery.Selection) {
			href, _ := s.Attr("href")
			m := memIDRe.FindStringSubmatch(href)
			if m == nil || seen[m[1]] {
				return
			}
			seen[m[1]] = true
			name := clean(s.Closest("td").Prev().Text())
			if i := strings.LastIndex(name, " - "); i > 0 { // drop trailing " - City"
				name = strings.TrimSpace(name[:i])
			}
			refs = append(refs, memberRef{ID: m[1], Name: name})
		})
	}
	return refs, nil
}

// fetchLead fetches and parses one member detail page. The contact block is a
// run of label/value cells; after flattening to a cell stream, each value is
// simply the cell following its label.
func fetchLead(ctx context.Context, c *Client, ref memberRef) (*Lead, error) {
	body, err := c.get(ctx, fmt.Sprintf("%sListingDetails.asp?MemID=%s", chamberBase, ref.ID))
	if err != nil {
		return nil, err
	}
	cs := cells(body)
	l := &Lead{MemID: ref.ID, Company: ref.Name, Source: "delhichamber.com"}
	for i, cell := range cs {
		label := strings.TrimRight(strings.ToLower(cell), ": ")
		switch {
		case strings.Contains(label, "contact person phone"):
			l.ContactPhone = next(cs, i)
		case strings.Contains(label, "contactperson"):
			l.Director = next(cs, i)
		case strings.Contains(label, "business type"):
			l.BusinessType = next(cs, i)
		case strings.Contains(label, "year established"):
			l.YearEst = yearRe.FindString(next(cs, i))
		case strings.Contains(label, "address"):
			l.Address = next(cs, i)
		case strings.Contains(label, "website"):
			if v := next(cs, i); strings.HasPrefix(strings.ToLower(v), "http") {
				l.Website = v
			}
		case label == "phone":
			l.Phone = next(cs, i)
		case strings.Contains(label, "about the company") && l.Company == "" && i > 0:
			l.Company = cs[i-1] // fallback if the list page had no name
		}
	}
	return l, nil
}

// next returns the cell after index i, unless that cell is itself a label
// (ends with ":"), which means the field's value was empty.
func next(cs []string, i int) string {
	if i+1 >= len(cs) {
		return ""
	}
	v := cs[i+1]
	if strings.HasSuffix(v, ":") {
		return ""
	}
	return v
}

// cells flattens HTML into a stream of non-empty cell strings: cell-closing
// tags become delimiters, all other tags are dropped, and intra-cell
// whitespace (including newlines) collapses to single spaces. This is robust
// to the directory's deeply nested tables and to values that render on a
// different source line than their label.
func cells(raw string) []string {
	delims := strings.NewReplacer(
		"</td>", "\t", "</TD>", "\t", "</th>", "\t", "</TH>", "\t",
		"</tr>", "\t", "</TR>", "\t", "</table>", "\t", "</TABLE>", "\t",
		"<br>", "\t", "<br/>", "\t", "<br />", "\t",
	)
	s := delims.Replace(raw)
	s = tagRe.ReplaceAllString(s, "")
	s = html.UnescapeString(s)
	var out []string
	for _, p := range strings.Split(s, "\t") {
		if f := clean(p); f != "" {
			out = append(out, f)
		}
	}
	return out
}

// clean collapses all whitespace (spaces, tabs, newlines) to single spaces.
func clean(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
