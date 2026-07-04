# proto-sitemap

Parse sitemaps (and sitemap indices) against the standards from https://www.sitemaps.org/protocol.html and as used by Google Search eg https://developers.google.com/search/docs/crawling-indexing/sitemaps/build-sitemap using github.com/accretional/gluon/v2 and github.com/accretional/xmile, similarly to how we handled https://github.com/accretional/xmile/blob/main/formats/rss-2.0.ebnf

Set up full round trip testing and validation against real sitemaps, use go 1.26, bootstrap your CLAUDE.md and general project structure (scripts, dirs, impl) from xmile. Keep iterating and working autonomously until we get the full thing working. Takes notes as you go in docs/decisions/ and docs/claude-worklog/.
