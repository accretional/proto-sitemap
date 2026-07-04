// Package formats holds the sitemap format spec as data — an EBNF element
// vocabulary (sitemap.ebnf), compiled on demand into a proto descriptor by
// xmile's EBNF_VOCAB front-end (service.CompileGrammar). The structure lives
// here as data, exactly as xmile keeps rss-2.0.ebnf; the only Go a format needs
// is the semantics the vocabulary grammar doesn't carry (service/validate.go).
package formats

import _ "embed"

// Sitemap is the Sitemaps 0.9 schema grammar (both the urlset and sitemapindex
// roots). service.Schema compiles it on first use.
//
//go:embed sitemap.ebnf
var Sitemap []byte
