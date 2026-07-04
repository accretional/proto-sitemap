# proto-sitemap architecture

One engine (xmile's), one compiler (gluon's); the sitemap format is data. A
Sitemaps-0.9 document — a `<urlset>` of URLs or a `<sitemapindex>` of child
sitemaps — is parsed into xmile's generic XML AST and projected into a typed
sitemap AST, and the AST serializes back to a document (a faithful round-trip).

```
                         LEVELS (borrowed from xmile's ADR 0008)
  L0  XML itself ......... xmile: lang/xml.ebnf + the one parser
  L1  schema languages ... xmile: EBNF-vocab front-end (service.CompileGrammar)
  L2  the sitemap format . proto-sitemap: formats/sitemap.ebnf  (DATA, compiled on demand)
```

proto-sitemap owns only L2 (the format as data) plus the few Go rules a CFG
cannot state; xmile owns L0/L1.

## The flow

```
 formats/sitemap.ebnf ─▶ service.CompileSchema(EBNF_VOCAB) ─▶ gluon compiler.Compile
   (urlset · sitemapindex                                          │
    · url · sitemap · loc · …)                                     ▼
                                        a proto FileDescriptor, one message per rule
                                        cached by service.Schema()  { Open:true,
                                                                      PreValidate:validateSitemap }
                                                     │
 bytes ─▶ xmile Parser.Parse ─▶ generic Xml/Tag AST ─┼─ Process ─▶ schema.Project ─▶ typed AST
        (well-formed XML)      (homogeneous, lossless)│              (match by local     (Urlset / Sitemapindex,
                                                      │               name; open)         resolved by root name)
                                                      ├─ Generate ─▶ bytes   (inverse of Parse)
                                                      └─ Conformance / Lint ─▶ warnings
```

- **One parser, never re-implemented.** Every document is parsed once by xmile's
  universal XML parser into the homogeneous `Xml`/`Tag` AST. The schema only
  decides the projection target; it never changes parsing.
- **Projection is xmile's one generic walk.** `schema.Project` places each element
  into the descriptor's matching field *by local name*, so the namespaced sitemap
  core (`{...}/0.9`) lines up with its local-named messages. Because the schema is
  **open**, unmodeled markup — Google's namespaced `image:`/`video:`/`news:`/
  `xhtml:link` extensions, or anything else — is tolerated (passed through), not
  rejected. The full untyped tree is always available from `Parse`.
- **Two roots, one descriptor.** gluon emits a message for every grammar rule, and
  `Process` resolves the root type by the document's root element name, so the
  single compiled descriptor projects both `<urlset>` and `<sitemapindex>`.
- **Validity is a separate, soft pass.** `validateSitemap` (hard) decides a
  document is a sitemap at all; `Conformance`/`Lint` (soft) report the value rules
  real sitemaps commonly bend. See `service/validate.go` and ADR 0001.

## Why open, not namespace-extensible

RSS 2.0's core is *unprefixed*, so xmile projects it with `nsExtensible`: a
namespaced element is a foreign extension, tolerated but dropped. A sitemap's core
is the opposite — it lives *in* a namespace — so `nsExtensible` would treat the
sitemap core itself as foreign and project nothing. The right knob is xmile's
`Open` schema (the one it uses for the minimal OOXML docx/xlsx schemas): match the
modeled core by local name, tolerate everything else. This is the single most
important design decision; see `docs/decisions/0001`.

## Round-trip

`RoundTrip` (`service/roundtrip.go`) is the invariant the tests and corpus runner
gate on: `Parse(Generate(Parse(b))) == Parse(b)`, compared at the **canonical
infoset** — the encoding declaration cleared (xmile's `Generate` always emits
UTF-8) and consecutive character-data runs coalesced — exactly as xmile's own
generate gate compares. It is faithful at the infoset level, not byte-for-byte:
entity spelling, quote style, and insignificant whitespace are not recorded.

## Where things live

| Path | Role |
|---|---|
| `formats/sitemap.ebnf`, `formats/embed.go` | L2 — the sitemap format as data |
| `service/sitemap.go` | schema compile+cache, `Parse`/`Process`/`Generate` |
| `service/validate.go` | CFG-inexpressible rules (hard pre-check + soft conformance) |
| `service/roundtrip.go` | canonical-infoset round-trip |
| `cmd/sitemapparse`, `cmd/sitemapgenerate` | CLIs |
| `testing/` | real-sitemap corpus fetcher + runner |
| `docs/decisions/0001-*` | the design record |
