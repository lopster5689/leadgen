package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	"golang.org/x/net/html/charset"
	"golang.org/x/time/rate"
)

// Client is a polite HTTP fetcher: one shared rate limiter across all requests,
// an identifying User-Agent, a per-request timeout, and a response-size cap.
type Client struct {
	hc      *http.Client
	limiter *rate.Limiter
	ua      string
}

func newClient(rps float64, timeout time.Duration) *Client {
	return &Client{
		hc:      &http.Client{Timeout: timeout},
		limiter: rate.NewLimiter(rate.Limit(rps), 1),
		ua:      "HostoLeadGen/1.0 (+mailto:rosh.s568@gmail.com)",
	}
}

const maxBody = 2 << 20 // 2 MiB

func (c *Client) get(ctx context.Context, url string) (string, error) {
	if err := c.limiter.Wait(ctx); err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", c.ua)
	resp, err := c.hc.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		io.Copy(io.Discard, resp.Body)
		return "", fmt.Errorf("status %d", resp.StatusCode)
	}
	// The directory is served as Windows-1252; decode to UTF-8 (via the
	// Content-Type / meta charset) so output is valid UTF-8 for Excel.
	src := io.LimitReader(resp.Body, maxBody)
	utf8r, err := charset.NewReader(src, resp.Header.Get("Content-Type"))
	if err != nil {
		utf8r = src
	}
	b, err := io.ReadAll(utf8r)
	return string(b), err
}

// checkSite probes a company website and reports a coarse status plus its
// <title>. The title is the cheap "is this still them?" signal — a Delhi
// exporter whose page title reads "Tangshan HD Caster Wheels Co., China"
// jumps out without any LLM. (A live-but-hijacked site still reads "live"
// here; that judgement is an LLM pass over the shortlist.)
func (c *Client) checkSite(ctx context.Context, url string) (status, title string) {
	if strings.TrimSpace(url) == "" {
		return "none", ""
	}
	body, err := c.get(ctx, url)
	if err != nil {
		return "dead", ""
	}
	if doc, derr := goquery.NewDocumentFromReader(strings.NewReader(body)); derr == nil {
		title = clean(doc.Find("title").First().Text())
	}
	return "live", title
}
