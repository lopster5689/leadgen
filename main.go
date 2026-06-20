// Command leadgen is a multi-source lead pipeline for the Hosto business.
//
//	leadgen crawl [source] [flags]   scrape a source into the deduped pool
//	leadgen next  [-n 50]            hand out the next batch of fresh leads
//	leadgen status                   pool health: fresh / delivered / by source
//	leadgen sources                  list available sources
//	leadgen dump <MemID>             debug: print a parsed detail page
//
// Operating loop: `next` until the pool runs dry, then `crawl <another source>`
// to refill. Every run appends to runs.jsonl and ANALYSIS_LOG.md.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sync/errgroup"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	switch os.Args[1] {
	case "crawl":
		cmdCrawl(os.Args[2:])
	case "next":
		cmdNext(os.Args[2:])
	case "push":
		cmdPush(os.Args[2:])
	case "status":
		cmdStatus(os.Args[2:])
	case "sources":
		cmdSources()
	case "dump":
		cmdDump(os.Args[2:])
	default:
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `leadgen - Hosto lead pipeline

  leadgen crawl [source] [-letters A-Z] [-checksites] [-rps N]   scrape a source into the pool
  leadgen next  [-n 50]                                          hand out the next fresh batch
  leadgen push  [-n N] [-endpoint URL]                          push fresh leads into the Twenty hub (idempotent)
  leadgen status                                                 pool health
  leadgen sources                                               list available sources
  leadgen dump <MemID>                                          debug a detail page

Default source: delhichamber. Pool: pool.csv. Batches: ./batches/.
`)
}

func signalCtx() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}

func cmdCrawl(args []string) {
	srcName := "delhichamber"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		srcName, args = args[0], args[1:]
	}
	fs := flag.NewFlagSet("crawl", flag.ExitOnError)
	letters := fs.String("letters", "ABCDEFGHIJKLMNOPQRSTUVWXYZ", "list pages to crawl (source-specific)")
	conc := fs.Int("concurrency", 6, "concurrent detail fetches")
	rps := fs.Float64("rps", 4, "requests per second")
	check := fs.Bool("checksites", true, "probe each company website")
	timeout := fs.Duration("timeout", 15*time.Second, "per-request timeout")
	poolPath := fs.String("pool", "pool.csv", "master pool CSV")
	siteURL := fs.String("url", "", "target URL (for the 'website' source)")
	fs.Parse(args)
	websiteURL = *siteURL

	src, ok := buildSource(srcName, *letters)
	if !ok {
		log.Fatalf("unknown source %q (try: leadgen sources)", srcName)
	}
	if n, err := importLegacy(*poolPath); err != nil {
		log.Fatalf("seed pool: %v", err)
	} else if n > 0 {
		log.Printf("seeded pool with %d existing leads", n)
	}

	existing, err := readPool(*poolPath)
	if err != nil {
		log.Fatalf("read pool: %v", err)
	}
	seenKey := map[string]bool{}
	seenID := map[string]bool{}
	for _, l := range existing {
		seenKey[l.Key] = true
		seenID[l.Source+"|"+l.MemID] = true
	}

	ctx, stop := signalCtx()
	defer stop()
	c := newClient(*rps, *timeout)

	log.Printf("[%s] discovering members…", srcName)
	refs, err := src.Discover(ctx, c)
	if err != nil {
		log.Fatalf("discovery: %v", err)
	}
	log.Printf("[%s] found %d members", srcName, len(refs))

	var todo []memberRef
	for _, r := range refs {
		if seenID[srcName+"|"+r.ID] || seenKey[normKey(r.Name)] {
			continue
		}
		todo = append(todo, r)
	}
	log.Printf("[%s] %d new to fetch (concurrency=%d, rps=%.1f, checksites=%v)…", srcName, len(todo), *conc, *rps, *check)

	app, err := newPoolAppender(*poolPath)
	if err != nil {
		log.Fatalf("pool append: %v", err)
	}
	defer app.close()

	results := make(chan *Lead)
	var collected int
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for l := range results {
			if err := app.append(l); err != nil {
				log.Printf("append %s: %v", l.MemID, err)
			}
			collected++
			if collected%25 == 0 {
				log.Printf("…%d collected", collected)
			}
		}
	}()

	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(*conc)
	for _, r := range todo {
		g.Go(func() error {
			l, err := src.Fetch(gctx, c, r)
			if err != nil {
				log.Printf("member %s: %v", r.ID, err)
				return nil
			}
			if *check {
				l.WebStatus, l.WebTitle = c.checkSite(gctx, l.Website)
			} else if l.Website == "" {
				l.WebStatus = "none"
			} else {
				l.WebStatus = "unchecked"
			}
			l.Source = srcName
			l.Key = normKey(l.Company)
			l.score()
			select {
			case results <- l:
			case <-gctx.Done():
				return gctx.Err()
			}
			return nil
		})
	}
	gerr := g.Wait()
	close(results)
	wg.Wait()
	if gerr != nil && gerr != context.Canceled {
		log.Printf("crawl interrupted: %v (partial results saved)", gerr)
	}

	all, _ := readPool(*poolPath)
	web, tags := tally(all)
	appendRunLog(filepath.Dir(*poolPath), RunStats{
		TS: time.Now().UTC().Format(time.RFC3339), Tool: "crawl", Source: srcName,
		Scope: "letters=" + *letters, Discovered: len(refs), New: collected,
		Total: len(all), CheckSites: *check, Web: web, Tags: tags,
	})
	log.Printf("[%s] added %d new; pool now holds %d leads", srcName, collected, len(all))
}

func cmdNext(args []string) {
	fs := flag.NewFlagSet("next", flag.ExitOnError)
	n := fs.Int("n", 50, "leads per batch")
	poolPath := fs.String("pool", "pool.csv", "master pool CSV")
	outdir := fs.String("dir", "batches", "batch output directory")
	fs.Parse(args)

	importLegacy(*poolPath)
	leads, err := readPool(*poolPath)
	if err != nil {
		log.Fatalf("read pool: %v", err)
	}
	if len(leads) == 0 {
		log.Fatal("pool is empty - run: leadgen crawl")
	}

	batch := nextBatch(leads, *n)
	if len(batch) == 0 {
		fmt.Println("No fresh leads left. Refill: leadgen crawl <source>  (see: leadgen sources)")
		return
	}

	os.MkdirAll(*outdir, 0o755)
	base := filepath.Join(*outdir, "batch-"+time.Now().Format("20060102-150405"))
	if err := writeBatch(base, batch); err != nil {
		log.Fatalf("write batch: %v", err)
	}

	stamp := time.Now().Format("2006-01-02")
	for _, l := range batch {
		l.Delivered = stamp
	}
	if err := writePool(*poolPath, leads); err != nil {
		log.Fatalf("update pool: %v", err)
	}

	batchReach, remaining, remReach := 0, 0, 0
	for _, l := range batch {
		if l.reachable() {
			batchReach++
		}
	}
	for _, l := range leads {
		if l.Delivered == "" {
			remaining++
			if l.reachable() {
				remReach++
			}
		}
	}

	logDelivery(filepath.Dir(*poolPath), len(batch), batchReach, remaining, base)
	fmt.Printf("delivered %d leads (%d reachable by mobile/email) -> %s.xlsx\n", len(batch), batchReach, base)
	fmt.Printf("pool: %d fresh remaining (%d reachable)\n", remaining, remReach)
	if remaining == 0 {
		fmt.Println("pool exhausted - add a source: leadgen crawl <source>")
	}
}

func cmdStatus(args []string) {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	poolPath := fs.String("pool", "pool.csv", "master pool CSV")
	fs.Parse(args)

	importLegacy(*poolPath)
	leads, err := readPool(*poolPath)
	if err != nil {
		log.Fatalf("read pool: %v", err)
	}
	if len(leads) == 0 {
		fmt.Println("pool is empty - run: leadgen crawl")
		return
	}
	s := statusOf(leads)
	fmt.Printf("pool: %s\n", *poolPath)
	fmt.Printf("  total      %d\n", s.Total)
	fmt.Printf("  delivered  %d\n", s.Delivered)
	fmt.Printf("  fresh      %d  (%d reachable by mobile/email)\n", s.Fresh, s.FreshReachable)
	fmt.Println("fresh by source:")
	for k, v := range s.BySource {
		fmt.Printf("  %-16s %d\n", k, v)
	}
	fmt.Println("fresh by fit tag:")
	for k, v := range s.ByTag {
		fmt.Printf("  %-16s %d\n", k, v)
	}
}

func cmdSources() {
	fmt.Println("available sources:")
	for _, n := range sourceNames {
		fmt.Println("  " + n)
	}
	fmt.Println("\nto add breadth: add a case to buildSource() in source.go (a Discover + Fetch pair)")
}

func cmdDump(args []string) {
	if len(args) == 0 {
		log.Fatal("usage: leadgen dump <MemID>")
	}
	ctx, stop := signalCtx()
	defer stop()
	c := newClient(5, 15*time.Second)
	body, err := c.get(ctx, fmt.Sprintf("%sListingDetails.asp?MemID=%s", chamberBase, args[0]))
	if err != nil {
		log.Fatal(err)
	}
	for i, cell := range cells(body) {
		fmt.Printf("%3d| %s\n", i, cell)
	}
}

// tally breaks leads down by web status and fit tag (for the run log).
func tally(leads []*Lead) (web, tags map[string]int) {
	web, tags = map[string]int{}, map[string]int{}
	for _, l := range leads {
		web[orNA(l.WebStatus)]++
		for _, t := range splitTags(l.Tags) {
			tags[t]++
		}
	}
	return web, tags
}

func splitTags(s string) []string {
	var out []string
	for _, t := range strings.Split(s, ",") {
		if t = strings.TrimSpace(t); t != "" {
			out = append(out, t)
		}
	}
	return out
}

func orNA(s string) string {
	if s == "" {
		return "(none)"
	}
	return s
}
