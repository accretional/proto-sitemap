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

## gRPC conversion (2026-08-28)

`sitemap.svc.v1.SitemapService/Expand`, with server reflection and
`grpc.health.v1.Health`.

This is the first `.proto` this repo has ever carried, and CLAUDE.md's standing
rule ("if you ever add a hand-written `.proto`, add a `regen.sh` and document it
here") is what it satisfies. The distinction that matters and is now written into
CLAUDE.md: the **format** still has no codegen — `formats/sitemap.ebnf` is data
compiled at runtime, and the typed AST never appears on the wire — while the
**service contract** is a proto like any other RPC surface.

`--use-http2` is required on the Cloud Run deploy. Without it Cloud Run
terminates HTTP/2 at the frontend and speaks HTTP/1.1 to the container, which a
gRPC server cannot answer; every RPC fails at the transport layer with nothing in
the application logs.

Message size is 64 MiB both ways rather than gRPC's 4 MiB default: a walk's
response carries every URL it found, and 50,000 entries is several MiB.

## Deployment (2026-08-27)

`./deploy.sh` mirrors webrisk-svc's conventions (same project `speax-498608`,
`us-central1`, the `embedder` Artifact Registry repo, a per-service SA with no
project roles, `--no-allow-unauthenticated`, git-SHA image tags), and runs
`gcloud auth configure-docker` itself — the first deploy failed on an
unauthenticated Artifact Registry push.

Live at `https://sitemap-svc-1041587693629.us-central1.run.app`.

Two sizing decisions worth keeping, both driven by what a walk actually does:

- **2Gi memory and concurrency 4, not the usual 80.** A walk can pull dozens of
  documents of up to 50 MiB each and project them into a typed AST; concurrent
  walks multiply that peak. 80-per-instance would OOM on large sitemaps.
- **900s request timeout, not the 300s default.** A real walk of a large site
  measured ~291s against these services — close enough to the default that a
  slightly larger site would be killed mid-flight, and the failure would look
  like a network error rather than a timeout.

Known quirk, environmental rather than ours: `GET /healthz` returns a Google 404
that never reaches the container. `webrisk`, an unrelated pre-existing service in
the same project, behaves identically, so something ahead of Cloud Run answers
non-API paths here. The API endpoint works and unauthenticated requests are still
refused with 403.

## Gate

`./LET_IT_RIP.sh` green: unit + adversarial gates, the 10 new
`cmd/sitemap-svc` walk tests, and the real-sitemap corpus. Live check against
`https://www.cloudflare.com/sitemap.xml` through the container returns URLs with
`lastmod` and a truthful `truncated`/`truncation_reason`.
