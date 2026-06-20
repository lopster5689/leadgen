package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// RunStats is one scrape's record, appended to runs.jsonl (machine-readable,
// for trend analysis across runs) and ANALYSIS_LOG.md (human-readable).
type RunStats struct {
	TS         string         `json:"ts"`
	Tool       string         `json:"tool"`
	Source     string         `json:"source"`
	Scope      string         `json:"scope"`
	Discovered int            `json:"discovered"`
	New        int            `json:"new"`
	Total      int            `json:"total"`
	CheckSites bool           `json:"checksites"`
	Web        map[string]int `json:"web"`
	Tags       map[string]int `json:"tags"`
}

const analysisHeader = `# Lead-gen analysis log

Append-only knowledge base for the Hosto lead pipeline. Every scrape and
enrichment run adds a dated block under "Run log". Read this before planning
outreach or changing the pipeline.

## Run log
`

// appendRunLog writes the run to runs.jsonl and ANALYSIS_LOG.md in dir,
// creating the markdown header on first use. Logging never aborts a run.
func appendRunLog(dir string, s RunStats) {
	if f, err := os.OpenFile(filepath.Join(dir, "runs.jsonl"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
		b, _ := json.Marshal(s)
		fmt.Fprintln(f, string(b))
		f.Close()
	}

	md := filepath.Join(dir, "ANALYSIS_LOG.md")
	if _, err := os.Stat(md); os.IsNotExist(err) {
		os.WriteFile(md, []byte(analysisHeader), 0o644)
	}
	f, err := os.OpenFile(md, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "\n### %s — %s crawl (%s, %s)\n", s.TS, s.Tool, s.Source, s.Scope)
	fmt.Fprintf(f, "- discovered %d | new this run %d | total %d | checksites=%v\n", s.Discovered, s.New, s.Total, s.CheckSites)
	fmt.Fprintf(f, "- web status: %s\n", kvLine(s.Web))
	fmt.Fprintf(f, "- fit tags: %s\n", kvLine(s.Tags))
}

// logDelivery records a `next` batch hand-out to the same knowledge base.
func logDelivery(dir string, delivered, reachable, remaining int, batchBase string) {
	ts := time.Now().UTC().Format(time.RFC3339)
	if f, err := os.OpenFile(filepath.Join(dir, "runs.jsonl"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
		b, _ := json.Marshal(map[string]any{
			"ts": ts, "tool": "next", "delivered": delivered,
			"reachable": reachable, "remaining": remaining, "batch": batchBase,
		})
		fmt.Fprintln(f, string(b))
		f.Close()
	}
	if f, err := os.OpenFile(filepath.Join(dir, "ANALYSIS_LOG.md"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
		fmt.Fprintf(f, "\n### %s — next (batch delivered)\n- delivered %d (%d reachable) -> %s.xlsx | %d fresh remaining\n",
			ts, delivered, reachable, batchBase, remaining)
		f.Close()
	}
}

func kvLine(m map[string]int) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s %d", k, m[k]))
	}
	return strings.Join(parts, ", ")
}
