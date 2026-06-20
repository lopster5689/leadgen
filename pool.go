package main

import (
	"encoding/csv"
	"os"
	"sort"
)

// poolHeaders is the master lead pool schema (matches Lead.row order).
var poolHeaders = []string{
	"MemID", "Company", "BusinessType", "YearEst", "Address", "Director",
	"Phone", "ContactPhone", "Website", "WebStatus", "WebTitle", "Tags",
	"Angle", "Source", "Email", "MobileFound", "Key", "Delivered", "Pushed",
}

func readPool(path string) ([]*Lead, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()
	r := csv.NewReader(f)
	r.FieldsPerRecord = -1
	rows, err := r.ReadAll()
	if err != nil {
		return nil, err
	}
	var out []*Lead
	for i, row := range rows {
		if i == 0 || len(row) == 0 {
			continue
		}
		out = append(out, leadFromRow(row))
	}
	return out, nil
}

func writePool(path string, leads []*Lead) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	defer w.Flush()
	if err := w.Write(poolHeaders); err != nil {
		return err
	}
	for _, l := range leads {
		if err := w.Write(l.row()); err != nil {
			return err
		}
	}
	return w.Error()
}

// poolAppender appends new leads to the pool one row at a time (crash-safe;
// existing rows are never rewritten during a crawl).
type poolAppender struct {
	f *os.File
	w *csv.Writer
}

func newPoolAppender(path string) (*poolAppender, error) {
	_, statErr := os.Stat(path)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	w := csv.NewWriter(f)
	if os.IsNotExist(statErr) {
		w.Write(poolHeaders)
		w.Flush()
	}
	return &poolAppender{f: f, w: w}, nil
}

func (p *poolAppender) append(l *Lead) error {
	if err := p.w.Write(l.row()); err != nil {
		return err
	}
	p.w.Flush()
	return p.w.Error()
}

func (p *poolAppender) close() { p.w.Flush(); p.f.Close() }

// importLegacy seeds the pool from the richest existing CSV (leads_enriched.csv,
// then leads.csv) the first time, so prior work isn't lost. No-op if the pool
// already exists or no legacy file is found.
func importLegacy(poolPath string) (int, error) {
	if _, err := os.Stat(poolPath); err == nil {
		return 0, nil
	}
	src := ""
	for _, c := range []string{"leads_enriched.csv", "leads.csv"} {
		if _, e := os.Stat(c); e == nil {
			src = c
			break
		}
	}
	if src == "" {
		return 0, nil
	}
	f, err := os.Open(src)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	r := csv.NewReader(f)
	r.FieldsPerRecord = -1
	rows, err := r.ReadAll()
	if err != nil || len(rows) < 2 {
		return 0, err
	}
	idx := map[string]int{}
	for i, h := range rows[0] {
		idx[h] = i
	}
	get := func(row []string, h string) string {
		if j, ok := idx[h]; ok && j < len(row) {
			return row[j]
		}
		return ""
	}
	seen := map[string]bool{}
	var leads []*Lead
	for _, row := range rows[1:] {
		l := &Lead{
			MemID: get(row, "MemID"), Company: get(row, "Company"),
			BusinessType: get(row, "BusinessType"), YearEst: get(row, "YearEst"),
			Address: get(row, "Address"), Director: get(row, "Director"),
			Phone: get(row, "Phone"), ContactPhone: get(row, "ContactPhone"),
			Website: get(row, "Website"), WebStatus: get(row, "WebStatus"),
			WebTitle: get(row, "WebTitle"), Tags: get(row, "Tags"),
			Angle: get(row, "Angle"), Source: get(row, "Source"),
			Email: get(row, "Email"), MobileFound: get(row, "MobileFound"),
		}
		l.Key = normKey(l.Company)
		if l.Key == "" || seen[l.Key] {
			continue
		}
		seen[l.Key] = true
		leads = append(leads, l)
	}
	return len(leads), writePool(poolPath, leads)
}

// nextBatch returns the top-n fresh (undelivered) leads by priority.
func nextBatch(leads []*Lead, n int) []*Lead {
	var fresh []*Lead
	for _, l := range leads {
		if l.Delivered == "" {
			fresh = append(fresh, l)
		}
	}
	sort.SliceStable(fresh, func(i, j int) bool {
		if pi, pj := fresh[i].priority(), fresh[j].priority(); pi != pj {
			return pi > pj
		}
		return fresh[i].Company < fresh[j].Company
	})
	if n > 0 && len(fresh) > n {
		fresh = fresh[:n]
	}
	return fresh
}

type poolStats struct {
	Total, Delivered, Fresh, FreshReachable int
	BySource, ByTag                         map[string]int
}

func statusOf(leads []*Lead) poolStats {
	s := poolStats{BySource: map[string]int{}, ByTag: map[string]int{}}
	s.Total = len(leads)
	for _, l := range leads {
		if l.Delivered != "" {
			s.Delivered++
			continue
		}
		s.Fresh++
		if l.reachable() {
			s.FreshReachable++
		}
		s.BySource[orNA(l.Source)]++
		for _, t := range splitTags(l.Tags) {
			s.ByTag[t]++
		}
	}
	return s
}
