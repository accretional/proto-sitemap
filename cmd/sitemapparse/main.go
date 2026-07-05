// Command sitemapparse parses a sitemap file (or stdin) with xmile's XML engine
// and prints the result. By default it projects the document into the typed
// sitemap AST (a urlset or sitemapindex message) and prints it as textproto;
// -generic prints the generic XML AST instead, and -lint prints the conformance
// warnings (protocol rules real sitemaps commonly bend). A not-well-formed or
// not-a-sitemap document exits non-zero.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"google.golang.org/protobuf/encoding/prototext"

	sitemap "github.com/accretional/proto-sitemap/service"
)

func main() {
	generic := flag.Bool("generic", false, "print the generic XML AST instead of the typed sitemap AST")
	lint := flag.Bool("lint", false, "print conformance warnings instead of the AST")
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

	switch {
	case *lint:
		warns, err := sitemap.Lint(p, src)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		if len(warns) == 0 {
			fmt.Println("conformant: no warnings")
			return
		}
		for _, w := range warns {
			fmt.Println("warning:", w)
		}
	case *generic:
		x, err := sitemap.Parse(p, string(src))
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Print(prototext.Format(x))
	default:
		msg, _, err := sitemap.Process(p, string(src))
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Print(prototext.Format(msg))
	}
}
