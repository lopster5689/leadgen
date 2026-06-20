package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

// leadgen push: send fresh (undelivered) pool leads into the Twenty hub.
// Reachability + tags are computed here (this is F2 routing folded into ingest),
// and dispatch is idempotent: existing dedupeKeys in Twenty are skipped, and the
// pool's Pushed column is the push ledger. Direct HTTP to Twenty (n8n webhook
// ingest is deferred until n8n queue mode is enabled — see leadhub/report.md).

var tagMapTwenty = map[string]string{
	"compliance": "COMPLIANCE", "whatsapp-crm": "WHATSAPP_CRM",
	"website+hosting": "WEBSITE_HOSTING", "website-recovery": "WEBSITE_RECOVERY", "fresh-setup": "FRESH_SETUP",
}
var webStatusSet = map[string]bool{"none": true, "dead": true, "live": true, "unchecked": true}

func cmdPush(args []string) {
	fs := flag.NewFlagSet("push", flag.ExitOnError)
	n := fs.Int("n", 0, "max new leads to push (0 = all undelivered)")
	endpoint := fs.String("endpoint", "http://localhost:3000", "Twenty base URL")
	tokenFile := fs.String("token-file", os.ExpandEnv("$HOME/.secrets/twenty.token"), "Twenty API token file")
	poolPath := fs.String("pool", "pool.csv", "master pool CSV")
	fs.Parse(args)

	tb, err := os.ReadFile(*tokenFile)
	if err != nil {
		log.Fatalf("token: %v", err)
	}
	token := strings.TrimSpace(string(tb))
	base := strings.TrimRight(*endpoint, "/")
	hc := &http.Client{Timeout: 30 * time.Second}

	call := func(method, path string, body any) (int, map[string]any) {
		var bb []byte
		if body != nil {
			bb, _ = json.Marshal(body)
		}
		for attempt := 0; ; attempt++ {
			var rdr io.Reader
			if body != nil {
				rdr = bytes.NewReader(bb)
			}
			req, _ := http.NewRequest(method, base+path, rdr)
			req.Header.Set("Authorization", "Bearer "+token)
			req.Header.Set("Content-Type", "application/json")
			resp, err := hc.Do(req)
			if err != nil {
				return 0, nil
			}
			rb, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if resp.StatusCode == 429 && attempt < 3 { // respect Twenty's ~100/min limit (Phase 0 lesson)
				time.Sleep(time.Duration(2<<attempt) * time.Second)
				continue
			}
			var out map[string]any
			json.Unmarshal(rb, &out)
			return resp.StatusCode, out
		}
	}

	// 1. existing dedupeKeys in Twenty (idempotency — never create a duplicate)
	existing := map[string]bool{}
	cursor := ""
	for {
		p := "/rest/leads?limit=200"
		if cursor != "" {
			p += "&starting_after=" + cursor
		}
		st, d := call("GET", p, nil)
		if st != 200 {
			log.Fatalf("twenty fetch failed: status %d", st)
		}
		data, _ := d["data"].(map[string]any)
		rows, _ := data["leads"].([]any)
		if len(rows) == 0 {
			break
		}
		for _, x := range rows {
			if m, ok := x.(map[string]any); ok {
				if dk, _ := m["dedupeKey"].(string); dk != "" {
					existing[dk] = true
				}
			}
		}
		pi, _ := d["pageInfo"].(map[string]any)
		if hn, _ := pi["hasNextPage"].(bool); !hn {
			break
		}
		cursor, _ = pi["endCursor"].(string)
		if cursor == "" {
			break
		}
	}
	log.Printf("twenty already holds %d leads (by dedupeKey)", len(existing))

	// 2. select undelivered pool leads; split new-to-hub vs already-there
	leads, err := readPool(*poolPath)
	if err != nil {
		log.Fatalf("pool: %v", err)
	}
	var todo, already []*Lead
	for _, l := range leads {
		if l.Pushed != "" {
			continue
		}
		if existing[l.Key] {
			already = append(already, l)
			continue
		}
		todo = append(todo, l)
		if *n > 0 && len(todo) >= *n {
			break
		}
	}
	log.Printf("undelivered: %d new-to-hub, %d already-in-hub", len(todo), len(already))

	// 3. create new leads in batches of 60; mark Pushed only on confirmed success
	stamp := time.Now().Format("2006-01-02")
	created, failed := 0, 0
	var errSamples []string
	for i := 0; i < len(todo); i += 60 {
		batch := todo[i:min(i+60, len(todo))]
		recs := make([]map[string]any, len(batch))
		for j, l := range batch {
			recs[j] = pushRecord(l)
		}
		st, _ := call("POST", "/rest/batch/leads", recs)
		if st < 300 {
			created += len(batch)
			for _, l := range batch {
				l.Pushed = stamp
			}
		} else { // isolate per-record on batch failure
			for _, l := range batch {
				s2, d2 := call("POST", "/rest/leads", pushRecord(l))
				if s2 < 300 {
					created++
					l.Pushed = stamp
				} else { // todo is pre-filtered to be new, so a 4xx here is a real failure (not a dup):
					failed++ // leave Pushed empty so the next run retries, and surface the error
					if len(errSamples) < 5 {
						errSamples = append(errSamples, fmt.Sprintf("%s -> %d %s", l.Key, s2, trunc(d2)))
					}
				}
			}
		}
		log.Printf("  …%d created, %d failed", created, failed)
	}
	for _, l := range already { // already in hub -> mark as pushed
		l.Pushed = stamp
	}

	if err := writePool(*poolPath, leads); err != nil {
		log.Fatalf("pool write: %v", err)
	}
	log.Printf("push complete: %d new leads created in Twenty; %d already-present marked pushed; %d failed (left for retry)", created, len(already), failed)
	for _, e := range errSamples {
		log.Printf("  fail: %s", e)
	}
}

// pushRecord maps a pool Lead to a Twenty `lead` record (reachability + tags + channels).
func pushRecord(l *Lead) map[string]any {
	email, mobile, channels, kinds := leadChannels(l)
	rec := map[string]any{
		"name": orElse(l.Company, l.Key), "company": l.Company, "director": l.Director,
		"addressText": l.Address, "businessType": l.BusinessType, "yearEst": l.YearEst,
		"website": l.Website, "angle": l.Angle, "source": l.Source, "dedupeKey": l.Key,
		"email": email, "primaryMobile": mobile, "channels": channels,
		"externalIds": map[string]any{}, "leadStatus": "NEW",
		"reachability": kinds, "leadTags": mapTags(l.Tags),
	}
	if ws := strings.ToLower(l.WebStatus); webStatusSet[ws] {
		rec["webStatus"] = strings.ToUpper(ws)
	}
	return rec
}

// leadChannels derives email/primaryMobile/channels/reachability from a pool Lead
// (mirrors the Phase-0 prepare_import.py normalization, in Go).
func leadChannels(l *Lead) (email, mobile string, channels []map[string]string, kinds []string) {
	channels = []map[string]string{}
	if e := strings.ToLower(strings.TrimSpace(l.Email)); e != "" && strings.Contains(e, "@") {
		email = e
		channels = append(channels, map[string]string{"kind": "email", "value": e})
		kinds = append(kinds, "EMAIL")
	}
	mobs := dedupStr(collect(mobileCandidates, asMobile, l.MobileFound, l.Phone, l.ContactPhone))
	for i, m := range mobs {
		channels = append(channels, map[string]string{"kind": "mobile", "value": m})
		if i == 0 {
			mobile = m
		}
	}
	if len(mobs) > 0 {
		kinds = append(kinds, "MOBILE")
	}
	lls := dedupStr(collect(splitNonDigits, asLandline, l.Phone, l.ContactPhone))
	for _, x := range lls {
		channels = append(channels, map[string]string{"kind": "landline", "value": x})
	}
	if len(lls) > 0 {
		kinds = append(kinds, "LANDLINE")
	}
	return email, mobile, channels, kinds
}

func collect(toks func(string) []string, fn func(string) string, fields ...string) []string {
	var out []string
	for _, f := range fields {
		for _, t := range toks(f) {
			if v := fn(t); v != "" {
				out = append(out, v)
			}
		}
	}
	return out
}

func asMobile(tok string) string {
	d := nonDigit.ReplaceAllString(tok, "")
	if len(d) > 10 && strings.HasPrefix(d, "91") {
		d = d[2:]
	}
	d = strings.TrimLeft(d, "0")
	if len(d) == 10 && d[0] >= '6' && d[0] <= '9' {
		return d
	}
	return ""
}

func asLandline(tok string) string {
	if asMobile(tok) != "" {
		return ""
	}
	d := nonDigit.ReplaceAllString(tok, "")
	if len(d) >= 6 && len(d) <= 13 {
		return d
	}
	return ""
}

func mapTags(tags string) []string {
	out := []string{}
	for _, t := range strings.Split(tags, ",") {
		if v, ok := tagMapTwenty[strings.TrimSpace(t)]; ok {
			out = append(out, v)
		}
	}
	return out
}

func dedupStr(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, x := range in {
		if !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	return out
}

func orElse(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

// trunc renders an error body compactly for logging.
func trunc(m map[string]any) string {
	b, _ := json.Marshal(m)
	s := string(b)
	if len(s) > 160 {
		s = s[:160]
	}
	return s
}
