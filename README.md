# proto-sitemap

Parse, validate, and round-trip [Sitemaps 0.9](https://www.sitemaps.org/protocol.html)
documents — a `<urlset>` of URLs or a `<sitemapindex>` of child sitemaps, as
consumed by [Google Search](https://developers.google.com/search/docs/crawling-indexing/sitemaps/build-sitemap).

It is built the way [xmile](https://github.com/accretional/xmile) handles RSS 2.0:
the sitemap *structure* is data — an EBNF element vocabulary (`formats/sitemap.ebnf`)
compiled on demand into a proto descriptor by [gluon](https://github.com/accretional/gluon)'s
compiler — while xmile's one XML engine does the parsing, projection, and
serialization. The only Go the format carries is the handful of rules a grammar
cannot state (a `<loc>` is required and ≤ 2048 chars, `<lastmod>` is a W3C
Datetime, `<priority>` is in [0.0, 1.0], …). See `ARCHITECTURE.md` and
`docs/decisions/`.

## Quick start

```bash
bash LET_IT_RIP.sh   # set up sibling deps, build, unit-test, and run the real-sitemap corpus
```

Requires **Go 1.26+**. `setup.sh` checks out the sibling module dependencies
(`xmile`, `gluon`, `proto-merge`) next to this repo; nothing else is needed.

## Library

```go
import sitemap "github.com/accretional/proto-sitemap/service"

p, _ := sitemap.Parser()

// Project into the typed sitemap AST (a urlset or sitemapindex message).
msg, root, err := sitemap.Process(p, src)   // root == "urlset" | "sitemapindex"

// Or parse to the generic, lossless XML AST and round-trip it.
x, _ := sitemap.Parse(p, src)
out, _ := sitemap.Generate(x)               // Parse(Generate(Parse(b))) == Parse(b)

// Validate against the protocol (well-formedness + is-a-sitemap are hard errors;
// everything else is a warning that real sitemaps commonly bend).
warnings, err := sitemap.Lint(p, []byte(src))
```

## CLI

```bash
go run ./cmd/sitemapparse    sitemap.xml            # typed AST (textproto)
go run ./cmd/sitemapparse -generic sitemap.xml      # generic XML AST
go run ./cmd/sitemapparse -lint    sitemap.xml      # conformance warnings
go run ./cmd/sitemapgenerate       sitemap.xml      # parse → AST → regenerated document
```

## What "handled" means

- **Both roots**, from one grammar and one compiled descriptor, resolved by the
  document's root element.
- **Google's namespaced extensions** (`image:`, `video:`, `news:`, `xhtml:link`)
  and any other unmodeled markup pass through losslessly — the schema is *open*, so
  the modeled core is typed and the rest is preserved in the generic AST.
- **Full round-trip**: every well-formed document serializes back to an equal AST
  at the canonical infoset, gated by both the unit tests and a corpus of real,
  public sitemaps (`go run ./testing`).
- **Conformance**: the sitemap namespace, `<loc>`/`<lastmod>`/`<changefreq>`/
  `<priority>` value rules, and the 50,000-entry / 50 MiB limits, reported as
  warnings.
