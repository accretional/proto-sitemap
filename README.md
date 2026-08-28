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

## Service

`cmd/sitemap-svc` serves the library over **gRPC** and adds the half the format
library deliberately does not carry: fetching. Given sitemap URLs it walks them —
following a `<sitemapindex>` to its children, breadth-first — and returns every
page URL it can reach.

```bash
docker build -t sitemap-svc .
docker run -p 8080:8080 sitemap-svc

# Server reflection is registered, so grpcurl needs no .proto on hand.
grpcurl -plaintext localhost:8080 list
# sitemap.svc.v1.SitemapService, grpc.health.v1.Health, ...

grpcurl -plaintext -d '{
  "sitemap_urls": ["https://www.cloudflare.com/sitemap.xml"],
  "crawl_delay_seconds": 1,
  "limits": {"max_urls": 5000, "max_sitemaps": 200, "max_depth": 5}
}' localhost:8080 sitemap.svc.v1.SitemapService/Expand
```

```jsonc
{
  "urls": [{"loc": "...", "lastmod": "...", "sourceSitemap": "..."}],
  "sitemapsFetched": 3,
  "truncated": false,
  "errors": [{"url": "...", "error": "HTTP 404"}],  // recorded, never fatal
  "elapsedMs": 812
}
```

Against the deployed service, swap plaintext for TLS and an ID token:

```bash
grpcurl -H "authorization: Bearer $(gcloud auth print-identity-token)" \
  -d '{"sitemap_urls":["https://www.cloudflare.com/sitemap.xml"],"limits":{"max_urls":5}}' \
  sitemap-svc-1041587693629.us-central1.run.app:443 \
  sitemap.svc.v1.SitemapService/Expand
```

`./deploy.sh` ships it to Cloud Run (project `speax-498608`, `us-central1`):
scale-to-zero, IAM-authenticated, **`--use-http2` (required for gRPC)**, and a
runtime identity with no project roles — the service only makes outbound HTTP
and needs nothing from GCP.

Sizing differs from a small service, because the work does: 2Gi and a *low*
per-instance concurrency (concurrent walks multiply peak memory, and a walk can
pull dozens of documents of up to 50 MiB each), and a 900s request timeout
rather than Cloud Run's 300s default — a real walk of a large site measured
~291s, which the default would have killed mid-flight.

What the service owns, because the format does not:

- **Gzip.** Sitemaps are commonly served as `.xml.gz`; payloads are gunzipped by
  magic bytes (covering both the URL suffix and `Content-Encoding`) and the
  output is capped, so a gzip bomb cannot exhaust memory.
- **Budgets.** An index may list 50,000 sitemaps of 50,000 URLs each, so every
  walk is bounded by `max_urls` / `max_sitemaps` / `max_depth`, and a truncated
  response says so and why rather than silently returning a prefix.
- **Crawl-delay.** When the caller passes one through from robots.txt, fetches to
  that host are serialized and spaced by it; otherwise they run concurrently.
- **Cycles and cross-host children.** A document is never fetched twice (indexes
  do point at each other), and a child on another host is refused by default —
  the protocol's own cross-submission rule, and the shape an untrusted index
  would use to aim the fetcher elsewhere. `-allow-cross-host` permits it.
- **Partial failure.** A dead or non-sitemap child is recorded in `errors` and
  the walk continues; a site serving HTML at `/sitemap.xml` is expected, not a
  bug.

The service contract is [`proto/sitemap_service.proto`](proto/sitemap_service.proto)
— the only proto this repo carries, and it describes the *service*, not the
format. The sitemap format remains data (`formats/sitemap.ebnf`), compiled to a
descriptor at runtime; the typed AST never appears on the wire. `./regen.sh`
regenerates the Go bindings into `proto/pb/`.

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
