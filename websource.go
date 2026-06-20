package main

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"regexp"
	"strings"
)

// website source: scrape a single site for contact info. Portfolios/SPAs often
// render contact details from a JS bundle, so we fetch the page AND its
// same-origin .js/.json assets and extract emails (HTML + bundles, filtered) and
// phones (precise: only tel:/wa.me links). Set the target with `crawl website -url`.
var websiteURL string

var (
	emailRe   = regexp.MustCompile(`(?i)[a-z0-9._%+\-]+@[a-z0-9.\-]+\.[a-z]{2,}`)
	assetRe   = regexp.MustCompile(`(?i)(?:src|href)="([^"]+\.(?:js|json))(?:\?[^"]*)?"`)
	titleRe   = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
	telLinkRe = regexp.MustCompile(`(?i)(?:tel:|wa\.me/|api\.whatsapp\.com/send\?phone=)\+?([0-9][0-9 ()\-]{6,})`)
)

// email domains that are almost always noise inside JS bundles / markup.
var emailNoiseDomain = map[string]bool{
	"example.com": true, "sentry.io": true, "sentry-cdn.com": true, "w3.org": true,
	"schema.org": true, "googleapis.com": true, "gstatic.com": true, "wixpress.com": true,
	"jsdelivr.net": true, "npmjs.org": true, "fontawesome.com": true, "googletagmanager.com": true,
}
var emailPrefer = map[string]bool{
	"gmail.com": true, "proton.me": true, "protonmail.com": true, "outlook.com": true,
	"hotmail.com": true, "yahoo.com": true, "icloud.com": true,
}

// localparts that are placeholders/non-mailboxes, never a real outreach target.
var emailPlaceholder = map[string]bool{
	"visitor": true, "you": true, "your": true, "youremail": true, "your-email": true,
	"name": true, "yourname": true, "your-name": true, "firstname": true, "lastname": true,
	"fullname": true, "example": true, "user": true, "username": true, "someone": true,
	"somebody": true, "test": true, "testing": true, "demo": true, "sample": true,
	"email": true, "mail": true, "placeholder": true, "foo": true, "bar": true,
	"johndoe": true, "john": true, "jane": true, "noreply": true, "no-reply": true,
	"donotreply": true, "do-not-reply": true,
}

func discoverWebsite(ctx context.Context, c *Client) ([]memberRef, error) {
	if strings.TrimSpace(websiteURL) == "" {
		return nil, fmt.Errorf("website source needs -url <site>")
	}
	u := websiteURL
	if !strings.HasPrefix(u, "http") {
		u = "https://" + u
	}
	return []memberRef{{ID: u, Name: hostOf(u)}}, nil
}

func fetchWebsite(ctx context.Context, c *Client, ref memberRef) (*Lead, error) {
	base := ref.ID
	html0, err := c.get(ctx, base)
	if err != nil {
		return nil, err
	}

	// 1) emails: from the page + same-origin js/json bundles
	blobs := []string{html0}
	seen := map[string]bool{}
	for _, m := range assetRe.FindAllStringSubmatch(html0, -1) {
		asset := absURL(base, m[1])
		if asset == "" || seen[asset] || hostOf(asset) != hostOf(base) {
			continue
		}
		seen[asset] = true
		if len(seen) > 12 { // cap asset fetches
			break
		}
		if b, e := c.get(ctx, asset); e == nil {
			blobs = append(blobs, b)
		}
	}
	emails := pickEmails(strings.Join(blobs, "\n"), hostOf(base))

	// 2) phones: only from explicit tel:/wa.me links (precise; avoids bundle noise)
	var phones []string
	pseen := map[string]bool{}
	for _, m := range telLinkRe.FindAllStringSubmatch(html0, -1) {
		if p := asMobile(m[1]); p != "" && !pseen[p] {
			pseen[p] = true
			phones = append(phones, p)
		}
	}

	log.Printf("[website] %s: %d email(s) %v, %d phone(s) %v", hostOf(base), len(emails), emails, len(phones), phones)

	title := ""
	if m := titleRe.FindStringSubmatch(html0); m != nil {
		title = clean(m[1])
	}
	l := &Lead{
		MemID: hostOf(base), Company: title, Source: hostOf(base), Website: base,
	}
	if len(emails) > 0 {
		l.Email = emails[0]
	}
	if len(phones) > 0 {
		l.MobileFound = phones[0]
		l.Phone = strings.Join(phones, ", ")
	}
	return l, nil
}

// pickEmails filters raw matches and orders best-first (site domain, then common
// personal providers, then the rest), dropping known bundle/markup noise.
func pickEmails(blob, host string) []string {
	root := rootDomain(host)
	seen := map[string]bool{}
	var site, pref, other []string
	for _, e := range emailRe.FindAllString(blob, -1) {
		e = strings.ToLower(strings.Trim(e, ".-_"))
		at := strings.LastIndex(e, "@")
		if at < 1 {
			continue
		}
		dom := e[at+1:]
		if emailNoiseDomain[dom] || emailPlaceholder[e[:at]] || strings.HasSuffix(e, ".png") ||
			strings.HasSuffix(e, ".jpg") || strings.HasSuffix(e, ".svg") || strings.HasSuffix(e, ".gif") ||
			strings.HasSuffix(e, ".webp") || len(e) > 60 || seen[e] {
			continue
		}
		seen[e] = true
		switch {
		case root != "" && strings.HasSuffix(dom, root):
			site = append(site, e)
		case emailPrefer[dom]:
			pref = append(pref, e)
		default:
			other = append(other, e)
		}
	}
	return append(append(site, pref...), other...)
}

func absURL(base, ref string) string {
	b, err1 := url.Parse(base)
	r, err2 := url.Parse(ref)
	if err1 != nil || err2 != nil || b == nil {
		return ""
	}
	return b.ResolveReference(r).String()
}

func hostOf(s string) string {
	u, err := url.Parse(s)
	if err != nil || u == nil {
		return s
	}
	return strings.TrimPrefix(u.Host, "www.")
}

// rootDomain returns the registrable-ish root (last two labels) for matching.
func rootDomain(host string) string {
	parts := strings.Split(host, ".")
	if len(parts) >= 2 {
		return strings.Join(parts[len(parts)-2:], ".")
	}
	return host
}
