# proto-sitemap

A parser and round-trip serializer for [Sitemaps 0.9](https://www.sitemaps.org/protocol.html)
documents — a `<urlset>` or a `<sitemapindex>`, as consumed by
[Google Search](https://developers.google.com/search/docs/crawling-indexing/sitemaps/build-sitemap).
It rides [xmile](https://github.com/accretional/xmile)'s XML engine and
[gluon](https://github.com/accretional/gluon)'s schema compiler; the sitemap
*structure* is data (`formats/sitemap.ebnf`), exactly as xmile keeps
`rss-2.0.ebnf`. The design record lives in `docs/decisions/`.

## Build Discipline

**This is the most important section.**

- **NEVER build/test/run code outside of `setup.sh`, `build.sh`, `test.sh`,
  `LET_IT_RIP.sh`.**
- **NEVER commit or push without running `./LET_IT_RIP.sh` first, and never if it
  is failing or has skipped a gate.**
- Scripts are idempotent and chained:
  `build.sh` → `setup.sh`; `test.sh` → `build.sh`; `LET_IT_RIP.sh` → `test.sh`.
- **The sitemap structure lives in the grammar, not in Go.** `formats/sitemap.ebnf`
  is the vocabulary (which elements nest in which); it is compiled to a proto
  descriptor at runtime by xmile's `service.CompileGrammar`. Never hand-code the
  element structure in Go. The only Go a format may carry is the irreducible,
  CFG-inexpressible semantics (`service/validate.go`).
- **There is no codegen step.** Unlike xmile, proto-sitemap generates and commits
  no proto: the format is *data*, compiled on demand. `build.sh` only sets up and
  builds. (If you ever add a hand-written `.proto` or a generated artifact, add a
  `regen.sh` and document it here — there should be NO stale document.)
- Any change to the code must be reflected in the docs. There should be NO stale
  document in the project.

## Pipeline

```
formats/sitemap.ebnf  (the sitemap vocabulary, data)
        │  service.CompileSchema(EBNF_VOCAB)  →  gluon compiler.Compile
        ▼
a proto FileDescriptor (messages Urlset, Sitemapindex, Url, Sitemap, Loc, …)
        │  cached in service.Schema()  (Open=true, PreValidate=validateSitemap)
        ▼
bytes ─▶ xmile Parser.Parse ─▶ generic Xml/Tag AST ─▶ schema.Project ─▶ typed AST
   (well-formed XML)          (lossless, round-trips)  (by local name; open)   (Urlset/Sitemapindex message)

bytes ─▶ Lint ─▶ well-formed? ─▶ is-a-sitemap? ─▶ Conformance warnings
Xml   ─▶ Generate ─▶ bytes   (inverse of Parse; RoundTrip checks the invariant)
```

## Architecture

- **The format is data; the engine is xmile's.** proto-sitemap does not define an
  XML grammar or a parser. It compiles `formats/sitemap.ebnf` into a descriptor
  through xmile's EBNF-vocab front-end, parses bytes with xmile's one XML parser
  into the generic `Xml`/`Tag` AST, and projects that AST into the typed sitemap
  message with xmile's generic `project` walk. See ADR 0001.
- **Two roots, one vocabulary.** A sitemap file is either a `<urlset>` or a
  `<sitemapindex>`. gluon emits a message for *every* grammar rule (no reachability
  pruning), and xmile's `Process` resolves the document root by element name, so
  one compiled descriptor serves both roots — `urlset` is written first only to
  satisfy the "start rule first" convention.
- **Open, not namespace-extensible.** The sitemap *core* vocabulary lives in the
  default namespace (`xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"`), unlike
  RSS whose core is unprefixed. xmile's projector matches the core by local name
  (namespace-agnostic). With `nsExtensible` it would treat every namespaced
  element — including the namespaced sitemap core — as a foreign extension and skip
  it, projecting *nothing*. So the schema is compiled **`Open`**: the modeled core
  is typed by local name and any unmodeled markup (Google's `image:`/`video:`/
  `news:`/`xhtml:link` extensions, or a stray element) passes through instead of
  being rejected. The full untyped tree is always available from `Parse`. ADR 0001.
- **CFG-inexpressible rules are the only Go a format carries** (`service/validate.go`):
  the structural pre-check `validateSitemap` (the root is `<urlset>`/`<sitemapindex>`;
  wired in as the Schema's `PreValidate`, it decides a document is a sitemap at all)
  and the soft conformance rules `Conformance`/`Lint` (the sitemap namespace; `<loc>`
  required, ≤ 2048 chars, an absolute URL; `<lastmod>` a W3C Datetime; `<changefreq>`
  in the closed set; `<priority>` in [0.0, 1.0]; ≤ 50,000 entries; ≤ 50 MiB). These
  mirror how xmile splits RSS's `validateRSS` (hard) from `RSSConformance` (soft):
  the hard rule gates, the soft ones are reported because real sitemaps bend them.
- **Round-trip is at the canonical infoset** (`service/roundtrip.go`). xmile's
  `Generate` is a faithful inverse of `Parse` but normalizes the encoding
  declaration to UTF-8 and coalesces character-data runs, so `RoundTrip` clears the
  encoding declaration and coalesces text before comparing — exactly as xmile's own
  generate gate does. The invariant: `Parse(Generate(Parse(b))) == Parse(b)`.

## Testing

- **Two gates.**
  - `go test ./...` is self-contained (`service/sitemap_test.go`): grammar compiles,
    both roots project, namespaced extensions pass through, non-sitemaps are
    rejected, round-trip is exact (including a lowercase encoding declaration), and
    conformance both passes clean and catches violations.
  - The corpus runner (`go run ./testing`, run by `test.sh`) fetches a curated set
    of **real, public sitemaps** (cached under `testing/corpus/`, gitignored) and,
    over each: **gates** that every well-formed document round-trips at the canonical
    infoset and that every sitemap root projects into the typed AST; **reports**
    (never gates) not-well-formed documents (a site serving HTML at `/sitemap.xml`
    is expected, not a bug) and conformance warnings. `go run ./testing fetch`
    refreshes only the corpus.

## Layout

| Path | Role |
|---|---|
| `formats/sitemap.ebnf` | the sitemap vocabulary (data; both roots) |
| `formats/embed.go` | embeds the spec for runtime compilation |
| `service/sitemap.go` | `Schema` (compile + cache), `Parse`/`Process`/`Generate`/`Parser` |
| `service/validate.go` | CFG-inexpressible rules: `validateSitemap` (hard), `Conformance`/`Lint` (soft) |
| `service/roundtrip.go` | `RoundTrip` + canonical-infoset comparison |
| `service/sitemap_test.go` | self-contained unit gate |
| `cmd/sitemapparse/` | CLI: doc → typed AST (`-generic` AST, `-lint` warnings) |
| `cmd/sitemapgenerate/` | CLI: doc → AST → regenerated doc (round-trips through Generate) |
| `testing/` | one corpus fetcher + runner (gates round-trip + projection) |
| `docs/decisions/` | ADRs (0001 = the architecture) |
| `docs/claude-worklog/` | build notes |
| `ARCHITECTURE.md` | data-flow overview |

## Dependencies

`go.mod` pins the local-module dependencies via `replace => ../<dep>`: `xmile`
(the engine), `gluon` (the compiler xmile rides), and `proto-merge` (`xmile`
requires it, and a dependency's own `replace`s do not carry over transitively).
`setup.sh` checks all three out as sibling repos. Requires **Go 1.26+**.
