# Worklog 0001 — Session setup & architecture findings

Date: 2026-07-03

## Environment (done, persistent)
- macOS 15.2, arm64. No Homebrew (didn't need it).
- Xcode Command Line Tools installed via scripted `softwareupdate -i "Command Line Tools for Xcode-16.2"` (no sudo needed) → `/Library/Developer/CommandLineTools`. `git 2.39.5` works.
- `gh` 2.96.0 installed as standalone binary at `~/.local/bin/gh`. Authed as **fredxfred** (keyring), scopes: gist, read:org, repo, workflow. `gh auth setup-git` done (git credential helper wired).
- Go **1.26.4** installed at `~/.local/go` (darwin-arm64).
- `~/.zshrc` exports PATH with `~/.local/bin` and `~/.local/go/bin`.

## Repos cloned under /Volumes/wd_office4/repos
- `proto-sitemap` (target, public) — only README/LICENSE/.gitignore so far.
- `xmile` (private — user said OK to make public; not yet done) — the framework to bootstrap from.
- `gluon` (public) — the parser/compiler engine. **Use its `v2/` subpackages** (`github.com/accretional/gluon/v2/{pb,compiler,metaparser}`). gluon is a single module (`github.com/accretional/gluon`, go 1.25.5); `v2/` is a subdirectory, not a separate major module.
- NOTE: xmile's go.mod also `replace`s `github.com/accretional/merge => ../proto-merge` and requires `github.com/accretional/proto-expr`. May need to clone `accretional/proto-merge` when building against xmile.

## Task (from proto-sitemap/README.md)
Parse sitemaps + sitemap indices per sitemaps.org/protocol.html and Google Search sitemap docs, using gluon/v2 + xmile, the same way xmile handles `formats/rss-2.0.ebnf`. Full round-trip testing + validation against real sitemaps. Go 1.26. Bootstrap CLAUDE.md + project structure (scripts, dirs, impl) from xmile. Notes in docs/decisions/ and docs/claude-worklog/.

## Key architecture findings (xmile)
- **Formats are DATA.** A format = a spec file under `formats/` (e.g. `rss-2.0.ebnf`, an EBNF *element vocabulary*, NOT a character grammar). Compiled on demand to a proto descriptor via gluon's `compiler.Compile` through a per-language front-end in `service/language/` (`CompileGrammar` for EBNF-vocab, also `CompileDTD`/`CompileXSD`). No committed per-format proto.
- One universal XML parser (`lang/xml.ebnf` + generated lexer) → homogeneous `Xml`/`Tag` AST. A schema only decides the *projection* target; `service/engine.go` `project` is one generic walk that fills the descriptor's typed message.
- CFG-inexpressible rules live in a small Go file per format (RSS: `service/rss.go` → `validateRSS` wired as the format's `PreValidate`; `RSSConformance` soft rules; `ParseRSS`). Registered via `formatMeta` in `service/formats.go` (`nsExtensible`, `preValidate`, `open`).
- **Generate** (`service/generate.go`) is the inverse of Process: serializes the generic `Xml` AST back to bytes. Round-trip invariant: `parse(Generate(parse(b))) == parse(b)` at the infoset level (not byte-identical). Only the generic AST round-trips; typed projections are read-only views.
- Build discipline (xmile CLAUDE.md): NEVER build/test/run outside `setup.sh`/`build.sh`/`test.sh`/`serve.sh`/`LET_IT_RIP.sh`. NEVER edit generated files. `regen.sh` regenerates from grammars. Scripts chained: build→setup, test→build, LET_IT_RIP→test.

## rss-2.0.ebnf conventions (the model to copy)
- A rule = an element; body lists child element refs + attributes.
- Attribute written `at_<name>` → lowers to `string <name>` field (marker stripped).
- `text` = character content of a leaf → `string text` field.
- Any ref with no rule of its own → string field; a ref with a rule → message field.
- Start rule is first.

## Open design questions to resolve on resume
1. Does proto-sitemap **import xmile's `service` package** as a library (call its EBNF-vocab compile + Process + Generate on a local `formats/sitemap-*.ebnf`), or vendor/reimplement the thin pieces? Need to inspect xmile's exported API surface: `service.Format`, `service.Process`, `service.Generate`, `service/language.CompileGrammar`, `Schema`, `formatMeta`. Determine how a *downstream* repo registers a format whose spec lives outside xmile's embedded `formats/` FS.
2. Sitemap specifics: two vocabularies — `<urlset>` (urls: loc, lastmod, changefreq, priority) and `<sitemapindex>` (sitemaps: loc, lastmod). Plus Google extensions (image:, video:, news:, xhtml:link alternates) — decide scope, likely namespace-extensible like RSS. CFG-inexpressible rules: loc required + <=2048 chars, lastmod W3C Datetime, changefreq enum, priority 0.0–1.0 default 0.5, <=50k URLs / <=50MB uncompressed per file.
3. Real-corpus source for round-trip validation (fetch like xmile's `testing/fetch.go`).

## Next steps
1. (optional) Make xmile public: `gh repo edit accretional/xmile --visibility public --accept-visibility-change-consequences` (user authorized).
2. Clone `accretional/proto-merge` if needed for building xmile.
3. Read xmile `service/{formats.go,process.go,schema.go,generate.go,rss.go}` + `service/language/*.go` to nail the consumer API.
4. Scaffold proto-sitemap: go.mod (go 1.26, replaces to ../xmile, ../gluon, and deps), CLAUDE.md, scripts, formats/sitemap-*.ebnf, service impl, testing/ corpus fetch+runner, docs/decisions/0001.
5. Implement → round-trip test → validate against real sitemaps → iterate.
