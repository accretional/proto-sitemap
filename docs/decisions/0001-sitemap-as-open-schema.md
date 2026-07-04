# ADR 0001 — The sitemap format is data, projected as an *open* schema

Status: accepted (2026-07-03)

## Context

proto-sitemap must parse and round-trip Sitemaps-0.9 documents (`<urlset>` and
`<sitemapindex>`) "using gluon/v2 and xmile, similarly to how we handled
rss-2.0.ebnf." xmile's model (ADR 0008) is: one universal XML parser produces a
homogeneous `Xml`/`Tag` AST; a *format* is a spec file compiled on demand into a
proto descriptor; one generic walk projects the parsed tree into the typed
message; a small Go file carries only the CFG-inexpressible semantics. RSS 2.0 is
the worked example (`formats/rss-2.0.ebnf` + `service/rss.go`).

Two facts make sitemaps differ from RSS in ways that drive the design:

1. **A sitemap has two independent roots** (`<urlset>`, `<sitemapindex>`), where
   RSS has one (`<rss>`).
2. **The sitemap core vocabulary is itself namespaced.** Every conforming sitemap
   declares `xmlns="http://www.sitemaps.org/schemas/sitemap/0.9"` on its root, so
   `<urlset>`, `<url>`, `<loc>`, … all resolve *into* that namespace. RSS's core,
   by contrast, is unprefixed; only its extensions are namespaced.

## Decision

**Model the sitemap vocabulary as one EBNF file (`formats/sitemap.ebnf`) with both
roots, compiled to a descriptor at runtime, and project it as an `Open` schema —
not an `nsExtensible` one.**

- **Two roots, one descriptor.** gluon's `GrammarToAST` emits a `KindRule` (hence a
  message) for *every* rule with no reachability pruning, and xmile's `Process`
  resolves the root type by the document's root element name. So a single grammar
  containing top-level `urlset` and `sitemapindex` rules yields both `Urlset` and
  `Sitemapindex` messages, and either root projects. `urlset` is first only to
  satisfy the "start rule first" convention.

- **Open, not namespace-extensible.** xmile's projector matches elements by *local*
  name, so the namespaced sitemap core lines up with its local-named messages
  regardless of the namespace. The extensibility knob then decides what happens to
  markup with *no* matching field:
  - `nsExtensible` (the RSS rule) treats **any namespaced** element as a foreign
    extension and skips it. Applied to sitemaps — whose core *is* namespaced — it
    would skip the entire core and project nothing. **Rejected.**
  - `Open` (the rule xmile uses for its minimal OOXML docx/xlsx schemas) types the
    modeled markup and tolerates the rest, in any namespace. Applied here: the core
    (`url`, `loc`, `lastmod`, …) is typed by local name; Google's namespaced
    `image:`/`video:`/`news:`/`xhtml:link` extensions — and any other unmodeled
    markup — pass through instead of being rejected. **Chosen.**

- **CFG-inexpressible rules split hard/soft**, mirroring RSS's `validateRSS` vs
  `RSSConformance`:
  - *hard* (`validateSitemap`, the Schema's `PreValidate`): the root is `<urlset>`
    or `<sitemapindex>`. This is what makes a document a sitemap at all.
  - *soft* (`Conformance`/`Lint`, reported not enforced): the sitemap namespace;
    `<loc>` present, ≤ 2048 chars, an absolute URL; `<lastmod>` a W3C Datetime;
    `<changefreq>` in the closed set; `<priority>` in [0.0, 1.0]; ≤ 50,000 entries;
    ≤ 50 MiB uncompressed. Real sitemaps bend these, so they are warnings.

- **Round-trip at the canonical infoset.** `RoundTrip` compares
  `Parse(Generate(Parse(b)))` with `Parse(b)` after clearing the encoding
  declaration (Generate emits UTF-8) and coalescing character-data runs — the same
  canonicalization xmile's own generate gate applies.

## Consequences

- Adding sitemap extension typing later (e.g. a typed `image:image`) means adding
  rules to the grammar; nothing in Go changes. Until then extensions round-trip
  losslessly (they are in the generic AST) but are not in the typed projection.
- Because the schema is open, genuinely malformed *core* markup (a misspelled
  `<lastmodd>`) is tolerated by projection rather than rejected; it is caught by
  `Conformance`, not by `Process`. This matches the protocol's own leniency and
  xmile's "projection is the loosest reading; validity is a separate pass."
- The observed real-world variant `https://…/schemas/sitemap/0.9` (Cloudflare) is
  reported by `Conformance`, not rejected — the local-name match still types it.
