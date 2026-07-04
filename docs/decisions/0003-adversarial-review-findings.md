# ADR 0003 — Adversarial review: the CFG-expressiveness claim is wrong-framed, and five code defects

Status: **accepted, fixes applied** (2026-07-03). Resolves the review checklist
ADR 0002 left open. The demonstrations are now permanent **gating** tests in
`service/adversarial_test.go` (they pass with the fixes below). See §5 for the
resolution of each finding.

## What was reviewed

Two asks: (1) adversarially validate proto-sitemap, and (2) test the claim —
carried in `CLAUDE.md`, ADR 0001, `service/sitemap.go`, `service/validate.go`,
and `formats/sitemap.ebnf` — that the rules in `validate.go` live in Go because
"a CFG cannot express" them. ADR 0002 had already walked most of that back but
was marked draft pending this pass, and flagged one unresolved high-value attack:
that gluon might parse with a **PEG** (which recognizes some non-context-free
languages), making the whole "CFG can't express X" frame use the wrong formalism.

Both were investigated against the actual gluon and xmile source. The verdict:
**the skepticism is correct — the CFG-inexpressibility framing is largely a
category error — and separately, the value rules that _are_ in Go are
under-implemented in five concrete, demonstrated ways.**

## 1. The formalism question, settled against source

**gluon is a CFG recognizer, not a PEG.** Its real parser (`gluon/lexkit/
parse_ast.go`, reused by v2's CST engine) is backtracking recursive descent over
the ISO-14977 EBNF core: sequence, alternation, optional, `{}` repetition, group,
terminal, nonterminal, char-range. Alternation is **longest-match**
(`matchAlternation` tries every alternative and keeps the furthest), not PEG's
ordered first-match. There are **no syntactic predicates (`&`/`!`), no
memoization, no semantic actions, and no counted/bounded repetition.** So ADR
0002's headline worry is _refuted_: the engine's ceiling is at most
context-free, and — because choice is longest-match, not prioritized — it is a
plain CFG, not a PEG. It cannot sneak up on `aⁿbⁿcⁿ`. (Bounded repetition, value
predicates, and name-equality are all impossible in the grammar; gluon's only
escape hatch is a hand-registered Go `TokenMatchFunc`, i.e. not the grammar.)

**But the sitemap grammar never recognizes a single byte — so "can a CFG parse
this?" is the wrong question entirely.** `formats/sitemap.ebnf` is compiled by
xmile's `EBNF_VOCAB` front-end (`CompileGrammar`) into a **proto
`FileDescriptorProto`**. That descriptor is consumed _only_ by the projection
walk (`xmile/service/engine.go`, `project`), which restructures an
**already-parsed** `Tag` tree by matching element **local names**. All byte-level
recognition is done once, by xmile's fixed `lang/xml.ebnf`, before any sitemap
code runs. The vocabulary `GrammarDescriptor` is handed to `GrammarToAST` and
then discarded; it is never installed on a parser, never fed source text.

Consequently the premise "these rules are in Go because a CFG can't express
them" is a **category error**: the sitemap grammar is not a string recognizer, so
CFG expressive power is not why _anything_ is in `validate.go`. The real reasons
are mundane and were there all along:

- **Leaves are opaque `text` by design.** The `EBNF_VOCAB` front-end lowers any
  leaf element to a `string` field; it never looks inside `<priority>` or
  `<lastmod>`, so value grammars have nowhere to live in this pipeline regardless
  of expressiveness.
- **Projection is loose by design.** With `schema.Open = true`, `project` never
  rejects: unmatched markup is skipped, and element **order and cardinality are
  ignored outright** (content-model matching exists in xmile — `MatchContentModel`
  in DTD-validating mode — but the Open projection path does not call it). So even
  genuinely context-free constraints ("exactly one `<loc>`", "≤ 1 of each child")
  are unenforced not because a CFG can't state them but because this path chooses
  not to check.
- **The soft rules are soft by spec.** Real sitemaps bend them, so they are
  warnings, by intent.

### Rule-by-rule resolution of ADR 0002's checklist

| Rule | Formal class | Why it is really in Go |
|---|---|---|
| `<changefreq>` enum | regular | opaque leaf; front-end never inspects leaf text |
| `<lastmod>` W3C datetime | regular | opaque leaf |
| `<loc>` ≤ 2048 chars / absolute URL | regular-ish | opaque leaf |
| `<priority>` ≤ 1.0 | **regular** (verbal "arithmetic ⇒ not CF" was wrong) | opaque leaf |
| ≤ 50,000 entries | **regular** (fixed bound ⇒ finite counter; _not_ `aⁿbⁿcⁿ`) | Open projection ignores cardinality |
| ≤ 50 MiB | below the element abstraction (a byte-length fact) | not a grammar concern at any level |
| exactly one `<loc>`, ≤1 each, unordered | **context-free** (bounded ⇒ even regular) | Open projection ignores order/cardinality |
| root in `.../0.9` namespace | context-sensitive | genuinely non-CF — but see below |
| tag-name agreement `<a>…</a>` | non-CF (`ww` copy language) | genuinely non-CF — but see below |

Two clarifications ADR 0002 got _almost_ right:

1. The only two genuinely non-context-free constraints (**namespace scoping** and
   **start/end tag-name agreement**) are **not proto-sitemap's rules at all** —
   they live one level down in xmile, enforced as Go tree walks
   (`xmile/service/namespace.go`; `xmile/service/wellformed.go` `walkWF`, whose
   `WFError` says "end tag `</%s>` does not match start tag `<%s>`"). xmile's
   `lang/xml.ebnf` deliberately over-accepts `<a></b>` and its own header cites
   the copy-language reason. proto-sitemap inherits these for free; they should
   not be cited as _its_ CFG boundary.
2. proto-sitemap's one _hard_ rule, `validateSitemap` ("root is `<urlset>` or
   `<sitemapindex>`"), is **regular** — a finite check on the root's local name.
   It is in Go because it is a `PreValidate` hook that gates projection, not
   because a CFG can't say "root ∈ {urlset, sitemapindex}".

**Conclusion:** keep `validate.go`, but fix the _rationale_ in the docs. The
honest statement is: "these rules are outside the grammar because the vocabulary
grammar is a projection schema over opaque-text leaves and the projection is
intentionally loose — **not** because a context-free grammar is incapable of
expressing them. The only truly non-CF constraints in the stack (tag matching,
namespace scoping) are xmile's, enforced by xmile as tree walks." See §3 for the
doc lines to change.

## 2. Code defects (each proven by a failing test)

Demonstrations in `service/adversarial_test.go`, gated behind
`PROTO_SITEMAP_ADVERSARIAL=1` so the shipped gate stays green. Each asserts the
spec-correct behavior, so its **failure is the proof**. Run:

```
PROTO_SITEMAP_ADVERSARIAL=1 go test ./service -run Adversarial -v
```

**D1 — `<priority>` accepts `NaN` and non-decimal spellings.** `validate.go` uses
`strconv.ParseFloat` then `f < 0.0 || f > 1.0`. For `NaN` both comparisons are
false, so `<priority>NaN</priority>` passes clean. `ParseFloat` also accepts
`1e0`, `1E-3`, `0x1p-1`, `+0.5`, `.5`, `1.` — none of which are the "number
between 0.0 and 1.0" the protocol defines. _Fix:_ reject `NaN`/`Inf`
(`math.IsNaN`/`math.IsInf`) and constrain the lexical form to a plain decimal
(regex `^(0(\.\d+)?|1(\.0+)?)$`, or at least `^\d*\.?\d+$` plus the NaN/Inf
guard). The constraint is _regular_ (ADR 0002 footnote 1 was right about the
language; the code just doesn't implement that language).

**D2 — `<loc>` accepts any absolute-URI scheme.** `checkLoc` accepts anything
`url.Parse` calls absolute, so `javascript:alert(1)`, `data:text/html,…`,
`file:///etc/passwd`, `mailto:…`, `foo:bar` all pass as valid locs and would be
handed to whatever consumes the sitemap — an injection sink if a consumer trusts
"conformant" locs. _Fix:_ require `u.Scheme` ∈ {`http`,`https`}.

**D3 — the 50,000-entry and 50 MiB caps are never enforced.** `Process` (the
projection entry point) checks neither; `MaxBytes` is only a soft warning in
`Lint`, and only against **source** length. A 50,001-entry document projects with
no error. _Fix:_ decide whether these are hard limits (then gate in `Process`) or
truly advisory (then say so plainly and stop implying enforcement).

**D4 (SECURITY) — reachable entity-expansion (billion-laughs) DoS.** xmile
expands internal general entities into content, and `Parse` exposes it: a
485-byte hostile `<!DOCTYPE>` with 12 nested entities expands to ~33 KB (68×);
growth is `2ⁿ`, so ~30 levels reaches gigabytes. proto-sitemap accepts a DOCTYPE
and enforces no expansion or pre-parse size cap — and `testing/fetch.go` parses
sitemaps fetched from **arbitrary remote servers**, so this is squarely in the
threat model. _Fix:_ sitemaps have no legitimate DOCTYPE — reject any document
with one before parsing, and/or impose a hard input-size cap up front and a total
expanded-size budget. (Root cause is in xmile's entity handling; proto-sitemap
should defend its own boundary regardless.)

**D5 (pinned, by-design) — a wrong-namespace `<urlset>` still projects as a valid
sitemap.** Only the local name is matched, so `xmlns="http://evil.example/…"`
projects clean with the namespace mismatch as a soft warning. This is ADR 0001's
accepted trade-off; the test pins it so any future tightening is a conscious
choice, not an accident.

## 3. Documentation corrections required (no stale docs)

The phrase "CFG-inexpressible" / "a CFG cannot express it" is inaccurate as the
rationale for `validate.go` and appears in: `CLAUDE.md` (Architecture bullet and
Layout row), `docs/decisions/0001-sitemap-as-open-schema.md` (Decision §
"CFG-inexpressible rules split hard/soft"), `service/sitemap.go` (package doc),
`service/validate.go` (file header), and `formats/sitemap.ebnf` (the "What is NOT
here (a CFG cannot express it)" comment). Each should be reworded to the honest
rationale in §1 ("outside the projection schema: opaque-text leaves + loose
projection + soft-by-spec; the only truly non-CF rules are xmile's tag-matching
and namespace scoping"). Tracked here rather than applied blind, because it
touches five files and the wording should be settled first.

## 4. Resolution (fixes applied)

- **§1 framing** — corrected in the prose (§3 files reworded: "out-of-grammar,
  not CFG-inexpressible"). ADR 0002 marked superseded-in-part.
- **D1 priority** — `validate.go` now checks a plain-decimal lexical form
  (`priorityRe`) plus the [0,1] range, rejecting `NaN`/`Inf`/exponent/hex/sign.
- **D2 loc** — `checkLoc` now requires an `http`/`https` scheme.
- **D3 limits** — split honestly: the 50 MiB byte size is now a **hard** boundary
  reject (`guard.go`, `MaxInputBytes`); the 50,000-entry cap stays a **soft**
  conformance warning (it is not a memory threat below the size cap, and the
  protocol asks readers to still process). Neither is "CFG-inexpressible."
- **D4 entity-expansion DoS** — closed at two layers: proto-sitemap refuses any
  DOCTYPE at the boundary (`guard.go`, sitemaps have none), and xmile caps entity
  expansion regardless (xmile ADR 0009).
- **D5 wrong-namespace** — unchanged (ADR 0001 trade-off), still pinned by a test.
- **Deep-nesting crash** (found during the xmile review this depends on) — xmile
  now rejects it with a depth guard (ADR 0009); a proto-sitemap gate covers it.

The boundary (`guard.go`) and validation gates are exercised by the permanent
tests in `service/adversarial_test.go`, run by `go test ./...`.

## 5. What holds up

The core architecture is sound and the review found no defect in it: two roots
from one descriptor, `Open` (not `nsExtensible`) for a namespaced core,
round-trip at the canonical infoset. `LET_IT_RIP.sh` is green and the demos are
opt-in, so the shipped gate is unaffected. The findings are about (a) an
inaccurate _rationale_ in the prose and (b) under-implemented _value_ checks and
one inherited _DoS_ surface — not about the projection design.
