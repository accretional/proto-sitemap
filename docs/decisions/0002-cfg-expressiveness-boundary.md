# ADR 0002 — Where the context-free boundary sits

Status: **reviewed — superseded in part by ADR 0003** (2026-07-03). The
adversarial pass has been done; its findings are in
`0003-adversarial-review-findings.md`. This ADR's rule-by-rule table stands (the
weak verbal claims it flagged were confirmed wrong), with two corrections from
0003: (a) gluon parses with a **CFG** recognizer, not a PEG, so the
"wrong-formalism" attack in the checklist is _refuted_, not merely open; and (b)
the deeper point is that the sitemap grammar is a **projection schema, not a
string recognizer at all**, so "can a CFG express X?" is the wrong question for
why any rule is in Go. Read this table as background; read 0003 for the verdict.
Written 2026-07-03.

## Question

`formats/sitemap.ebnf` is a grammar over the element vocabulary; the remaining
rules live in `service/validate.go`. Which of those remaining rules *genuinely
cannot* be expressed by a context-free grammar, and which are merely **kept out
of the grammar by design or practicality**? The first framing (given verbally,
and preserved below) conflated the two; this ADR separates them and flags the
weak claims for a later adversarial pass.

## The grammar's job (uncontested)

The CFG states containment: `urlset ⊃ url ⊃ {loc | lastmod | changefreq |
priority}` and `sitemapindex ⊃ sitemap ⊃ {loc | lastmod}`, leaves as opaque
`text`. That nesting is essentially all a CFG asserts here.

## Rule-by-rule: is it *theoretically* context-free?

| Rule (in `validate.go`) | Language view | CF in principle? | Why it's in Go |
|---|---|---|---|
| `<changefreq>` ∈ 7-value enum | finite set of strings | **yes** (regular) | leaves are opaque `text` by design |
| `<lastmod>` a W3C Datetime | fixed lexical grammar | **yes** (regular) | opaque leaves by design |
| `<loc>` ≤ 2048 chars | length bound | **yes** (finite ⇒ regular) | opaque leaves; bound is huge |
| `<loc>` an absolute URL | URI syntax | **mostly** (regular-ish) | opaque leaves by design |
| `<priority>` value ≤ 1.0 | decimal strings ≤ 1.0 | **yes** (regular¹) | opaque leaves; verbal claim overstated |
| ≤ 50,000 entries | fixed finite count | **yes** (finite ⇒ regular²) | 50,001-state grammar is absurd, not impossible |
| ≤ 50 MiB uncompressed | byte-length bound | n/a at this level³ | element grammar is above bytes |
| exactly one `<loc>`, ≤1 of each, unordered | bounded multiset + permutations | **yes** (finite⁴) | the order-tolerant `{...}` model discards it |
| root in the `.../0.9` namespace | scoped prefix binding | **no** (context-sensitive) | genuinely not CF |

¹ The set of decimal numerals denoting a value ≤ 1.0 is regular (a DFA: `0`
optionally `.` any digits; or `1` optionally `.` only zeros). So "priority ≤ 1.0"
as string recognition is regular — the verbal "arithmetic predicate, not
context-free" was wrong.
² A fixed upper bound is a *finite* constraint (a counter with 50,001 states),
hence regular — **not** the unbounded `aⁿbⁿcⁿ` case the verbal answer invoked.
³ A constraint on the concrete byte serialization, which the element-level
grammar cannot observe at all; "not CF" is the wrong lens — "below the
abstraction" is the right one.
⁴ With counts bounded (0/1 each, loc exactly 1), the set of accepted child
multisets is finite, so even the unordered form is regular — the
"Presburger/not context-free" framing only bites for *unbounded* counts.

## The honest conclusion

Only **two** constraints in play are genuinely non-context-free:

1. **Namespace scoping** — an `xmlns` binding is in scope for a whole subtree and
   the same name resolves differently under different in-scope declarations;
   context-sensitive by construction (xmile resolves it as a tree walk, not
   grammar).
2. **XML start/end tag-name agreement** (`<a>…</a>` — one level down, in xmile's
   character grammar, not ours): requiring the closing name to equal the opening
   name across an arbitrary span is the copy-language (`ww`) pattern, which is not
   context-free. xmile enforces it as a well-formedness walk for exactly this
   reason.

Everything else `validate.go` enforces is regular (enums, datetime, bounded
counts, decimal-value bounds) or below the grammar's abstraction (byte size). It
lives in Go for **practicality and design cleanliness** — opaque leaves, avoiding
astronomically large grammars, order tolerance — **not** because a CFG is
theoretically incapable. That distinction is what the verbal summary blurred.

## Adversarial review checklist (attack these later)

- [ ] **priority ≤ 1.0 is regular** — verify the DFA sketch (footnote 1) actually
      accepts exactly `[0,1]` decimals and rejects `1.1`, `2`, `01`, `1.00001`.
- [ ] **≤ 50,000 is regular, not `aⁿbⁿcⁿ`** — confirm the verbal "unbounded
      counting" claim is wrong for a fixed bound.
- [ ] **unordered bounded cardinality is finite/regular** — confirm "exactly one
      loc, ≤1 of each" is not actually Presburger/non-CF here.
- [ ] **datetime / enum / URL are regular sublanguages** — so keeping them in Go
      is a design choice; confirm none hide a genuinely non-CF requirement.
- [ ] **namespace scoping is genuinely context-sensitive** — or can a CFG over a
      *namespace-annotated* alphabet fake it? (xmile already resolves namespaces
      before projection — does that change the answer?)
- [ ] **tag-name agreement is non-CF** — verify the `ww` reduction; is XML with a
      bounded tag-name alphabet still non-CF?
- [ ] **byte-size framing** — is "≤ 50 MiB" best described as "not CF" or "below
      the abstraction"? Pick the precise statement.
- [ ] **WRONG-FORMALISM RISK (highest value):** gluon/xmile parse characters with
      a PEG/packrat-style engine, **not** a pure CFG. PEGs with syntactic
      predicates recognize some non-context-free languages (they can do `aⁿbⁿcⁿ`).
      If gluon supports lookahead/predicates, the entire "a CFG can't express X"
      framing uses the wrong model — the real boundary is "what gluon's formalism
      can express," which may be strictly larger. **Verify gluon's actual
      expressive class before trusting any claim above.**

## Provenance

This ADR records a claim made quickly in conversation and immediately flagged by
the user for adversarial review. Treat the table as hypotheses, not findings,
until the checklist is worked through.
