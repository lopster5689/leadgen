package main

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Lead is one prospect record and one row in the pool. Field order matches
// poolHeaders (see pool.go).
type Lead struct {
	MemID        string
	Company      string
	BusinessType string
	YearEst      string
	Address      string
	Director     string
	Phone        string
	ContactPhone string
	Website      string
	WebStatus    string
	WebTitle     string
	Tags         string
	Angle        string
	Source       string
	Email        string
	MobileFound  string
	Key          string // normalized company name, for cross-source dedup
	Delivered    string // date this lead was handed out in a batch ("next"); "" = fresh
	Pushed       string // date this lead was pushed to the Twenty hub ("push"); "" = not yet
}

func (l *Lead) row() []string {
	return []string{
		l.MemID, l.Company, l.BusinessType, l.YearEst, l.Address, l.Director,
		l.Phone, l.ContactPhone, l.Website, l.WebStatus, l.WebTitle, l.Tags,
		l.Angle, l.Source, l.Email, l.MobileFound, l.Key, l.Delivered, l.Pushed,
	}
}

func leadFromRow(r []string) *Lead {
	g := func(i int) string {
		if i < len(r) {
			return r[i]
		}
		return ""
	}
	return &Lead{
		MemID: g(0), Company: g(1), BusinessType: g(2), YearEst: g(3),
		Address: g(4), Director: g(5), Phone: g(6), ContactPhone: g(7),
		Website: g(8), WebStatus: g(9), WebTitle: g(10), Tags: g(11),
		Angle: g(12), Source: g(13), Email: g(14), MobileFound: g(15),
		Key: g(16), Delivered: g(17), Pushed: g(18),
	}
}

var pvtRe = regexp.MustCompile(`(?i)\bpvt\b|\bprivate\b|\(p\)`)

// score applies rule-based fit tags and a suggested outreach angle.
func (l *Lead) score() {
	var tags []string
	if pvtRe.MatchString(l.Company) {
		tags = append(tags, "compliance")
	}
	bt := strings.ToLower(l.BusinessType + " " + l.Company)
	exporter := strings.Contains(bt, "export") || strings.Contains(bt, "manufactur") || strings.Contains(bt, "trade")
	if exporter {
		tags = append(tags, "whatsapp-crm")
	}
	switch l.WebStatus {
	case "none":
		tags = append(tags, "website+hosting")
	case "dead":
		tags = append(tags, "website-recovery")
	}
	fresh := false
	if y, err := strconv.Atoi(l.YearEst); err == nil && y >= time.Now().Year()-2 {
		fresh = true
		tags = append(tags, "fresh-setup")
	}
	l.Tags = strings.Join(tags, ", ")

	switch {
	case l.WebStatus == "none" && exporter:
		l.Angle = "No website for an export business. Buyers can't find or vet you; pitch a site plus a WhatsApp catalogue."
	case l.WebStatus == "none":
		l.Angle = "No web presence; lead with a starter website and hosting."
	case l.WebStatus == "dead":
		l.Angle = "Listed website is down; recover it before a competitor grabs the domain."
	case fresh:
		l.Angle = "Young Pvt Ltd: early ROC filings are live now. Lead with compliance reminders, upsell site and CRM."
	default:
		l.Angle = "Pvt Ltd: annual MCA/ROC compliance reminder is the universal opener."
	}
}

// hasMobile reports whether s contains an Indian 10-digit mobile (6-9 lead),
// tolerating +91/91/0 prefixes and numbers written with internal spaces.
// Uses the same candidate logic as push.go's asMobile so leadgen and the hub agree.
func hasMobile(s string) bool {
	for _, c := range mobileCandidates(s) {
		if asMobile(c) != "" {
			return true
		}
	}
	return false
}

var nonDigit = regexp.MustCompile(`[^0-9]+`)

func splitNonDigits(s string) []string { return nonDigit.Split(s, -1) }

// mobileCandidates yields each digit run plus adjacent-run concatenations, so a
// mobile split by internal spaces ("98765 43210", "+91 98765 43210") is still
// recognized without merging numbers across strong separators.
func mobileCandidates(s string) []string {
	toks := splitNonDigits(s)
	out := append([]string{}, toks...)
	for i := 0; i+1 < len(toks); i++ {
		out = append(out, toks[i]+toks[i+1])
	}
	return out
}

// reachable: can this lead be contacted on a digital channel right now?
// Consults the same fields the hub ingest does (incl. ContactPhone) so leadgen's
// reachable count never disagrees with what push.go writes to Twenty.
func (l *Lead) reachable() bool {
	return l.Email != "" || l.MobileFound != "" || hasMobile(l.Phone) || hasMobile(l.ContactPhone)
}

// priority ranks fresh leads for delivery: reachable + high-fit first.
func (l *Lead) priority() int {
	s := 0
	if l.reachable() {
		s += 5
	}
	t := l.Tags
	if strings.Contains(t, "website-recovery") {
		s += 4
	}
	if strings.Contains(t, "website+hosting") && strings.Contains(t, "whatsapp-crm") {
		s += 3
	} else if strings.Contains(t, "website+hosting") {
		s += 2
	}
	if strings.Contains(t, "fresh-setup") {
		s += 2
	}
	if strings.Contains(t, "compliance") {
		s += 1
	}
	return s
}

var nonAlnum = regexp.MustCompile(`[^a-z0-9]+`)
var keyStop = map[string]bool{
	"pvt": true, "private": true, "ltd": true, "limited": true, "p": true,
	"co": true, "company": true, "inc": true, "the": true, "and": true, "india": true,
}

// normKey is the cross-source dedup key: lowercased company name with
// punctuation and legal/filler words removed.
func normKey(company string) string {
	s := nonAlnum.ReplaceAllString(strings.ToLower(company), " ")
	var out []string
	for _, w := range strings.Fields(s) {
		if !keyStop[w] {
			out = append(out, w)
		}
	}
	return strings.Join(out, " ")
}
