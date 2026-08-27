# 0003 — cmd/sitemap-svc: the fetching half, containerized

Date: 2026-08-27

## Why

proto-sitemap takes bytes. The pipeline it is being wired into
(domain → webrisk → robots.txt → sitemap → URL list) needs something that takes
*sitemap URLs* and returns page URLs, which means owning HTTP: gzip payloads,
recursion through `<sitemapindex>`, budgets, and crawl-delay. That is service
work, not format work, so it lives in `cmd/sitemap-svc` and the `service/`
package is untouched by it.

## What landed

- `cmd/sitemap-svc/` — HTTP/JSON service. `POST /v1/sitemap:expand`, `GET /healthz`.
  - `api.go` — wire types as **plain Go structs**, deliberately not protos
    (CLAUDE.md's no-codegen rule; the domain AST is already proto).
  - `fetch.go` — bounded GETs: 64 MiB wire cap, gunzip-by-magic-bytes capped at
    `MaxInputBytes+1` (gzip-bomb bound), 5-redirect cap, per-host serialization
    and spacing under a crawl delay, bounded concurrency without one.
  - `expand.go` — the breadth-first walk. Levels fetch concurrently and parse
    serially (one xmile parser stays on one goroutine, so its concurrency
    guarantees are never relied on).
- `Dockerfile` — builder runs `setup.sh` (the sibling `replace` deps), runtime is
  distroless/static. 26.5 MB.
- `service/guard.go` — the depth boundary, see ADR 0004 below.

## Findings worth keeping

**1. The depth guard was delegated to a decision that does not exist.**
`TestBoundary_RejectsDeepNesting` began failing the moment the sibling xmile
checkout moved forward. Root cause: xmile ADR 0009 (cited in `guard.go`,
`CLAUDE.md`, and ADR 0003) does not exist upstream and xmile carries no depth
cap. Fixed by owning the rule here (`MaxDepth`, `exceedsDepth`) — ADR 0004.

**2. The sibling `replace` model drifts silently.** The same checkout move also
left `go.sum` missing `go.starlark.net` (gluon grew a `builddep` package that
imports it), so a clean `go build` failed before any of this started. `go mod tidy`
fixed it — the diff is one `go.mod` line and two `go.sum` lines. Worth knowing
that `../gluon` is shared with sibling projects that move it independently: this
repo's build state depends on another repo's working tree.

**3. Don't hardcode the AST path to `<loc>`.** The compiled descriptor interposes
the grammar's alternation messages — a `<loc>` currently sits at
`url > alt1 > loc > text`. `alt1` is an artifact of how `formats/sitemap.ebnf`
spells the vocabulary, not of the sitemap format, so `expand.go` finds leaves by a
breadth-first search on element *name*. A hardcoded path would break silently on a
regrammar, yielding empty URLs rather than an error.

**4. Cross-host index children are refused by default.** It is the protocol's own
cross-submission rule, and it closes the shape where an untrusted `<sitemapindex>`
aims the fetcher at a host nobody pointed it at. `-allow-cross-host` opts in.

## Gate

`./LET_IT_RIP.sh` green: unit + adversarial gates, the 10 new
`cmd/sitemap-svc` walk tests, and the real-sitemap corpus. Live check against
`https://www.cloudflare.com/sitemap.xml` through the container returns URLs with
`lastmod` and a truthful `truncated`/`truncation_reason`.
