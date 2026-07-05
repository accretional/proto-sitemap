// Command sitemapgenerate round-trips a sitemap file (or stdin): it parses the
// document into the generic XML AST and serializes that AST back to a document
// with xmile's Generate — the inverse of parsing. The output is faithful at the
// infoset level (Parse(Generate(Parse(b))) == Parse(b)), not byte-identical.
// A not-well-formed document exits non-zero.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	sitemap "github.com/accretional/proto-sitemap/service"
)

func main() {
	flag.Parse()

	var src []byte
	var err error
	if args := flag.Args(); len(args) > 0 {
		src, err = os.ReadFile(args[0])
	} else {
		src, err = io.ReadAll(os.Stdin)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	p, err := sitemap.Parser()
	if err != nil {
		fmt.Fprintln(os.Stderr, "init:", err)
		os.Exit(1)
	}

	x, err := sitemap.Parse(p, string(src))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	out, err := sitemap.Generate(x)
	if err != nil {
		fmt.Fprintln(os.Stderr, "generate:", err)
		os.Exit(1)
	}
	os.Stdout.Write(out)
}
