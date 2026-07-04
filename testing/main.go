// Command testing is the corpus runner: it fetches a set of real, public
// sitemaps (cached, gitignored) and runs proto-sitemap over each. Two checks
// GATE (a non-zero exit): every well-formed document must round-trip exactly
// (Parse(Generate(Parse(b))) == Parse(b)), and every document whose root is a
// sitemap must project into the typed AST. Everything else is REPORTED, not
// gated — real-world sitemaps are messy (an occasional non-XML error page or an
// Atom feed served at /sitemap.xml is expected, not a bug), mirroring how xmile
// reports its rss/docx-web corpora. Conformance warnings are tallied and shown.
//
//	go run ./testing        fetch (first run) + all checks
//	go run ./testing fetch  refresh the corpus only
package main

import (
	"fmt"
	"os"
	"path/filepath"

	sitemap "github.com/accretional/proto-sitemap/service"
	"github.com/accretional/xmile/service"
)

func main() {
	fetchOnly := len(os.Args) > 1 && os.Args[1] == "fetch"

	fmt.Println("[corpus] fetching real sitemaps (cached after first run)")
	paths, err := fetchCorpus()
	if err != nil {
		fmt.Fprintln(os.Stderr, "fetch:", err)
		os.Exit(1)
	}
	if fetchOnly {
		fmt.Printf("[corpus] %d files cached in %s\n", len(paths), corpusDir)
		return
	}
	if len(paths) == 0 {
		fmt.Println("[corpus] no sitemaps available (offline?); skipping corpus checks (unit tests still gate)")
		return
	}

	p, err := service.Default()
	if err != nil {
		fmt.Fprintln(os.Stderr, "init parser:", err)
		os.Exit(1)
	}

	var (
		total, wellFormed, roundTripped int
		urlsets, indexes, withWarnings  int
		warnTotal                       int
		failures                        []string
	)
	fmt.Printf("\n[corpus] checking %d documents\n", len(paths))
	for _, path := range paths {
		total++
		name := filepath.Base(path)
		src, err := os.ReadFile(path)
		if err != nil {
			failures = append(failures, fmt.Sprintf("%s: read: %v", name, err))
			continue
		}

		x, err := p.Parse(string(src), false)
		if err != nil {
			fmt.Printf("  [reported] %s: not well-formed XML: %v\n", name, err)
			continue
		}
		wellFormed++

		// GATE 1: round-trip must be exact (canonical infoset) for every
		// well-formed document.
		ok, _, rerr := sitemap.RoundTrip(p, x)
		if rerr != nil {
			failures = append(failures, fmt.Sprintf("%s: round-trip: %v", name, rerr))
			continue
		}
		if !ok {
			failures = append(failures, fmt.Sprintf("%s: round-trip changed the AST", name))
			continue
		}
		roundTripped++

		// Classify + GATE 2: a sitemap root must project into the typed AST.
		_, root, xerr := sitemap.Process(p, string(src))
		if xerr != nil {
			fmt.Printf("  [reported] %s: not a sitemap root: %v\n", name, xerr)
			continue
		}
		switch root {
		case "urlset":
			urlsets++
		case "sitemapindex":
			indexes++
		}

		// Conformance (reported, not gated).
		if w := sitemap.Conformance(x); len(w) > 0 {
			withWarnings++
			warnTotal += len(w)
			fmt.Printf("  [warn] %s (%s): %d conformance warning(s), e.g. %s\n", name, root, len(w), w[0])
		}
	}

	fmt.Printf("\n[corpus] summary\n")
	fmt.Printf("  fetched:            %d\n", total)
	fmt.Printf("  well-formed XML:    %d\n", wellFormed)
	fmt.Printf("  round-tripped:      %d  (gate: == well-formed)\n", roundTripped)
	fmt.Printf("  urlsets / indexes:  %d / %d  (typed projection)\n", urlsets, indexes)
	fmt.Printf("  with warnings:      %d docs, %d warnings total (reported)\n", withWarnings, warnTotal)

	if len(failures) > 0 {
		fmt.Printf("\n[corpus] FAILED — %d gated failure(s):\n", len(failures))
		for _, f := range failures {
			fmt.Println("  -", f)
		}
		os.Exit(1)
	}
	fmt.Println("\n[corpus] OK")
}
