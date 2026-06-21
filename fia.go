package main

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// Faridabad Industries Association member directory (fiafaridabad.com): a public,
// server-rendered list of ~300 Faridabad (Delhi-NCR) manufacturers — prime ICP.
// The list page pairs each company (p.nameofindus > a, href view_details.php?id=N)
// with its address; the detail page adds email(s), website, and a mobile. Discover
// builds one ref per member (company+address embedded); Fetch enriches from detail.
const fiaBase = "https://fiafaridabad.com/fiamembers/"

var (
	fiaIDRe     = regexp.MustCompile(`view_details\.php\?id=(\d+)`)
	fiaEmailRe  = regexp.MustCompile(`[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}`)
	fiaMobileRe = regexp.MustCompile(`[6-9][0-9]{9}`)
)

func discoverFIA(ctx context.Context, c *Client) ([]memberRef, error) {
	body, err := c.get(ctx, fiaBase)
	if err != nil {
		return nil, fmt.Errorf("fia list: %w", err)
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(body))
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var refs []memberRef
	doc.Find("p.nameofindus a").Each(func(_ int, s *goquery.Selection) {
		href, _ := s.Attr("href")
		m := fiaIDRe.FindStringSubmatch(href)
		if m == nil || seen[m[1]] {
			return
		}
		company := clean(s.Text())
		if company == "" {
			return
		}
		seen[m[1]] = true
		block := s.Closest("div.industrydisplaymain")
		addr := clean(block.Find("p.addressdis").First().Text())
		refs = append(refs, memberRef{ID: m[1], Name: company,
			lead: &Lead{MemID: m[1], Company: company, Address: addr, Source: "fiafaridabad.com"}})
	})
	return refs, nil
}

func fetchFIA(ctx context.Context, c *Client, ref memberRef) (*Lead, error) {
	l := ref.lead
	if l == nil {
		l = &Lead{MemID: ref.ID, Company: ref.Name, Source: "fiafaridabad.com"}
	}
	body, err := c.get(ctx, fiaBase+"view_details.php?id="+ref.ID)
	if err != nil {
		return l, nil // keep the list-derived data even if the detail page fails
	}
	// emails — exclude the association's own domain, dedupe
	var emails []string
	for _, e := range fiaEmailRe.FindAllString(body, -1) {
		le := strings.ToLower(e)
		if strings.Contains(le, "fiafaridabad") || fiaHas(emails, le) {
			continue
		}
		emails = append(emails, le)
	}
	if len(emails) > 0 {
		l.Email = strings.Join(emails, ", ")
	}
	if l.Phone == "" {
		if mm := fiaMobileRe.FindString(body); mm != "" {
			l.Phone = mm
		}
	}
	// website — first external link that isn't the association / a social network
	if doc, derr := goquery.NewDocumentFromReader(strings.NewReader(body)); derr == nil {
		doc.Find("a[href]").EachWithBreak(func(_ int, s *goquery.Selection) bool {
			h, _ := s.Attr("href")
			lh := strings.ToLower(h)
			if strings.HasPrefix(lh, "http") && !strings.Contains(lh, "fiafaridabad") &&
				!strings.Contains(lh, "facebook") && !strings.Contains(lh, "twitter") &&
				!strings.Contains(lh, "linkedin") && !strings.Contains(lh, "instagram") &&
				!strings.Contains(lh, "google") && !strings.Contains(lh, "whatsapp") {
				l.Website = h
				return false
			}
			return true
		})
	}
	return l, nil
}

func fiaHas(ss []string, v string) bool {
	for _, s := range ss {
		if s == v {
			return true
		}
	}
	return false
}
