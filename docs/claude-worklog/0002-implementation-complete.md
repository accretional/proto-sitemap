# Worklog 0002 — Implementation complete

Date: 2026-07-03

## Status: working end-to-end. `./LET_IT_RIP.sh` is green.

Built proto-sitemap the way xmile handles RSS 2.0: the sitemap vocabulary is data
(`formats/sitemap.ebnf`), compiled at runtime by xmile's `service.CompileGrammar`;
xmile's one XML engine parses, projects, and serializes; only the
CFG-inexpressible rules are Go (`service/validate.go`).

## What was built
- `formats/sitemap.ebnf` — one grammar, both roots (`urlset`, `sitemapindex`) +
  `url`/`sitemap`/`loc`/`lastmod`/`changefreq`/`priority`.
- `service/sitemap.go` — `Schema()` (compile + cache), `Parse`/`Process`/`Generate`/`Parser`.
- `service/validate.go` — `validateSitemap` (hard PreValidate), `Conformance`/`Lint` (soft).
- `service/roundtrip.go` — `RoundTrip` + canonical-infoset comparison.
- `service/sitemap_test.go` — self-contained unit gate (10 tests, all pass).
- `cmd/sitemapparse` (typed / `-generic` / `-lint`), `cmd/sitemapgenerate` (round-trip).
- `testing/` — corpus fetcher + runner over 16 real public sitemaps.
- `setup.sh`/`build.sh`/`test.sh`/`LET_IT_RIP.sh`, `CLAUDE.md`, `ARCHITECTURE.md`,
  `docs/decisions/0001`, README rewritten.

## The one decision that mattered (ADR 0001)
The sitemap **core** vocabulary is itself namespaced (`xmlns=".../0.9"`), unlike
RSS whose core is unprefixed. xmile's projector, under `nsExtensible`, treats any
namespaced element as a foreign extension and skips it — which would skip the
whole sitemap core and project nothing. Fix: compile the schema **`Open`** (the
knob xmile uses for minimal OOXML schemas): match the core by local name, tolerate
(pass through) all other markup incl. Google's `image:`/`video:`/`news:`/`xhtml:`
extensions. Verified by `TestProcessToleratesExtensions`.

## Round-trip subtlety
Initial corpus run failed round-trip on 2 docs. Root cause: xmile's `Generate`
canonicalizes the XML declaration encoding to `UTF-8` and coalesces text runs
(documented behavior). A raw `proto.Equal` is stricter than xmile's actual
invariant. Fixed by comparing at the canonical infoset (clear encoding decl +
coalesce text), exactly as xmile's own generate gate (`testing/main.go`
`canonicalXML`). Now 16/16 well-formed corpus docs round-trip.

## Corpus result (16 real sitemaps)
16 fetched, 16 well-formed, **16 round-tripped**, 7 urlsets + 9 indexes projected,
1 reported conformance warning (Cloudflare serves the namespace as `https://…/0.9`
— tolerated via local-name match, warned by Conformance). Large docs included
(elastic 6.3 MB, postgres 4.5 MB, nodejs 1.6 MB) as a stress test. Corpus is
gitignored; re-fetched by `go run ./testing`.

## Environment notes (for future sessions)
- Toolchain installed under `~/.local` (go 1.26.4, gh 2.96.0); Xcode CLT via
  scripted softwareupdate. `~/.zshrc` has the PATH. gh authed as fredxfred.
- xmile is now **public** (user authorized flipping it). Sibling repos at
  /Volumes/wd_office4/repos: xmile, gluon, proto-merge, proto-sitemap.
- Nothing committed/pushed yet — awaiting user direction.

## Possible follow-ups (not required by the README)
- Typed models for Google extensions (add rules to the grammar; no Go change).
- A gRPC service layer like xmile's, if wanted.
- Robots.txt `Sitemap:` discovery.
