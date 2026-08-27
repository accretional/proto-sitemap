package main

// api.go — the HTTP/JSON surface of sitemap-svc.
//
// The wire types are plain Go structs, not protos. proto-sitemap deliberately
// generates and commits no .proto (CLAUDE.md: "the format is data, compiled on
// demand"), and a hand-written one would oblige a regen.sh and a codegen step
// for what is only a request/response DTO. The sitemap *domain* stays proto —
// the typed AST that Process returns — and only this transport envelope is Go.

// ExpandRequest asks the service to walk one or more sitemap URLs and return
// every page URL reachable from them.
type ExpandRequest struct {
	// SitemapURLs are the roots of the walk — typically the Sitemap: lines a
	// robots.txt declared. Both <urlset> and <sitemapindex> documents are
	// accepted; an index is followed to its children, breadth-first.
	SitemapURLs []string `json:"sitemap_urls"`
	// UserAgent identifies the fetcher to the origin. Defaults to DefaultUserAgent.
	UserAgent string `json:"user_agent,omitempty"`
	// CrawlDelaySeconds, when > 0, is honoured between successive fetches to the
	// same host (robots.txt Crawl-delay). It serializes that host's fetches.
	CrawlDelaySeconds float64 `json:"crawl_delay_seconds,omitempty"`
	// Limits bound the walk. Zero fields take the DefaultLimits value.
	Limits *Limits `json:"limits,omitempty"`
}

// Limits bound a walk. A sitemapindex may list 50,000 sitemaps of 50,000 URLs
// each, so an unbounded walk is 2.5 billion URLs; every field here has a
// non-zero default for that reason.
type Limits struct {
	MaxURLs     int `json:"max_urls,omitempty"`
	MaxSitemaps int `json:"max_sitemaps,omitempty"`
	MaxDepth    int `json:"max_depth,omitempty"`
}

// DefaultLimits are applied to any zero field of a request's Limits.
var DefaultLimits = Limits{
	MaxURLs:     50000, // the protocol's per-file entry cap, as a whole-walk cap
	MaxSitemaps: 200,   // enough for a large multi-part index, far below 50,000
	MaxDepth:    5,     // the protocol allows one index level; 5 tolerates abuse
}

// withDefaults returns l with every zero field replaced by its default.
func (l *Limits) withDefaults() Limits {
	out := DefaultLimits
	if l == nil {
		return out
	}
	if l.MaxURLs > 0 {
		out.MaxURLs = l.MaxURLs
	}
	if l.MaxSitemaps > 0 {
		out.MaxSitemaps = l.MaxSitemaps
	}
	if l.MaxDepth > 0 {
		out.MaxDepth = l.MaxDepth
	}
	return out
}

// URLEntry is one <url> from a <urlset>.
type URLEntry struct {
	Loc     string `json:"loc"`
	Lastmod string `json:"lastmod,omitempty"`
	// Source is the sitemap document this entry came from — useful when a walk
	// spans dozens of child sitemaps.
	Source string `json:"source_sitemap"`
}

// FetchError records one sitemap that could not be fetched or parsed. A walk
// reports these and continues: a site serving HTML at /sitemap.xml, or one dead
// child of a healthy index, should not fail the whole expansion.
type FetchError struct {
	URL   string `json:"url"`
	Error string `json:"error"`
}

// ExpandResponse is the result of a walk.
type ExpandResponse struct {
	URLs             []URLEntry   `json:"urls"`
	SitemapsFetched  int          `json:"sitemaps_fetched"`
	Truncated        bool         `json:"truncated"`
	TruncationReason string       `json:"truncation_reason,omitempty"`
	Errors           []FetchError `json:"errors,omitempty"`
	ElapsedMS        int64        `json:"elapsed_ms"`
}
