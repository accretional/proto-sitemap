# ADR 0004 — The nesting-depth boundary is owned here, not delegated to xmile

Status: **accepted, fix applied** (2026-08-27). Corrects one claim in ADR 0003 §5
and the `service/guard.go` header comment.

## Context

ADR 0003 closed the deep-nesting finding by delegating it upstream:

> **Deep-nesting crash** — xmile now rejects it with a depth guard (ADR 0009); a
> proto-sitemap gate covers it.

`service/guard.go` carried the matching comment ("Deep nesting is guarded by
xmile itself; see xmile ADR 0009"), and `CLAUDE.md` described the boundary as
"defense in depth over xmile's own parser guards (xmile ADR 0009:
entity-expansion + nesting-depth caps)". The gate — `TestBoundary_RejectsDeepNesting`
— passed, so the arrangement looked sound.

It was not. Working on `cmd/sitemap-svc` (which fetches sitemaps from arbitrary
hosts, exactly the untrusted-input case the boundary exists for) surfaced two
facts:

1. **xmile ADR 0009 does not exist.** xmile's `docs/decisions/` ends at 0008.
   The cited decision was never upstreamed, or was removed.
2. **xmile carries no depth cap.** A grep for a depth/nesting bound across
   xmile's source finds nothing that limits recursion.

`TestBoundary_RejectsDeepNesting` had been passing for an unrelated reason and
began failing the moment the sibling xmile checkout moved forward. A 20,000-level
document parsed without complaint.

This is the specific hazard of a `replace => ../dep` dependency: the guarantee a
gate depends on can leave upstream without any signal here beyond a test that
starts failing later, attributed to "drift".

## Decision

**proto-sitemap enforces its own nesting-depth boundary**, in `guard.go`,
alongside the size and DOCTYPE rejections — the two boundary rules it already
owned. `MaxDepth = 100`, checked by `exceedsDepth` on every
`Parse`/`Process`/`Lint`.

`exceedsDepth` is a structural pre-scan, not a second XML parser. It counts only
element open/close, skipping the constructs that may legitimately contain `<` or
`>` — comments, CDATA, processing instructions, declarations, and quoted
attribute values — and treats an unterminated construct as "pass through", since
the parser proper rejects it moments later.

100 is chosen against the format, not the parser: the sitemap core is three
levels (`urlset > url > loc`), and Google's deepest extension (`<video:video>`
with children, or `<xhtml:link>`) reaches five. Two orders of magnitude of
headroom keeps every real generator well inside the bound.

## Consequences

- The boundary no longer depends on an upstream guarantee that does not exist.
  All three hard rejections — size, DOCTYPE, depth — are now local, testable, and
  visible in one file.
- `TestBoundary_RejectsDeepNesting` passes for the stated reason.
  `TestBoundary_DepthGuardAcceptsRealShapes` pins the shapes that must *not* be
  refused (extensions, self-closing elements, `>` inside an attribute value,
  CDATA and comments containing angle brackets), and
  `TestBoundary_DepthGuardBoundary` pins the cap itself.
- ADR 0003's D4 note remains accurate on the DOCTYPE half; its parenthetical that
  "xmile caps entity expansion regardless (xmile ADR 0009)" rests on the same
  non-existent ADR and should be read as unverified. The DOCTYPE rejection at our
  boundary is what actually closes D4 — a document with no DOCTYPE has no
  general-entity declaration to expand.
- General lesson for this repo: a boundary rule delegated to a sibling `replace`
  dependency is not a boundary rule. If it gates untrusted input here, enforce it
  here.
