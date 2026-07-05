package sitemap

// validate.go — the sitemap semantics the vocabulary grammar does not carry,
// the analogue of xmile's service/rss.go: the structural pre-check
// (validateSitemap, wired in as the Schema's PreValidate — it decides whether a
// document is a sitemap at all), and the soft conformance rules (Conformance /
// Lint — the value constraints the protocol states but real-world sitemaps
// routinely bend). formats/sitemap.ebnf is a projection schema over opaque-text
// leaves; it states which elements nest, so leaf-value and cross-entry rules
// live here — NOT because a context-free grammar cannot express them (most are
// regular: enums, datetime, a decimal bound, a fixed entry cap), but because the
// projection deliberately does not check leaf content or cardinality. See
// docs/decisions/0003-adversarial-review-findings.md.

import (
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	xmlpb "github.com/accretional/xmile/proto/pb/xml"
	"github.com/accretional/xmile/service"
)

// Protocol limits (https://www.sitemaps.org/protocol.html): a single file lists
// at most 50,000 entries and is at most 50 MiB (52,428,800 bytes) uncompressed;
// a <loc> value is at most 2,048 characters.
const (
	MaxEntries = 50000
	MaxBytes   = 50 * 1024 * 1024
	MaxLocLen  = 2048
)

// priorityRe matches a plain decimal numeral: digits with an optional fraction,
// no sign/exponent/hex. validPriority adds the [0.0, 1.0] range check.
var priorityRe = regexp.MustCompile(`^[0-9]+(\.[0-9]+)?$`)

// changefreqValues is the closed set of <changefreq> values the protocol defines.
var changefreqValues = map[string]bool{
	"always": true, "hourly": true, "daily": true, "weekly": true,
	"monthly": true, "yearly": true, "never": true,
}

// validateSitemap is the structural pre-check wired in as the sitemap Schema's
// PreValidate: it decides whether a well-formed document is a sitemap at all.
// The root must be <urlset> or <sitemapindex> (matched by local name, so the
// namespace declaration is tolerated but not required here — Conformance warns
// when it is missing or non-standard). Everything softer is a conformance
// warning, not a hard rejection, mirroring how xmile reports real-world feeds.
func validateSitemap(doc *xmlpb.Xml) error {
	root := doc.GetRoot()
	if root == nil {
		return &service.ValidityError{Msg: "not a sitemap: document has no root element"}
	}
	switch localName(root) {
	case "urlset", "sitemapindex":
		return nil
	default:
		return &service.ValidityError{Msg: fmt.Sprintf("not a sitemap: root element is <%s>, want <urlset> or <sitemapindex>", root.GetName())}
	}
}

// Lint parses sitemap source and returns its conformance warnings — the
// real-world validation entry point. Input past the boundary is a hard error
// (*InputError: over 50 MiB, or carrying a DOCTYPE; see guard.go); malformed
// XML is a hard error (*service.WFError); a document that is not a sitemap is a
// hard error (*service.ValidityError); every soft rule (namespace, <loc>,
// <lastmod>, <changefreq>, <priority>, entry count) is a warning. A nil error
// with no warnings means fully conformant.
func Lint(p *service.Parser, src []byte) ([]string, error) {
	if err := guardSource(string(src)); err != nil {
		return nil, err
	}
	x, err := p.Parse(string(src), false)
	if err != nil {
		return nil, err
	}
	if err := validateSitemap(x); err != nil {
		return nil, err
	}
	return Conformance(x), nil
}

// Conformance returns the soft Sitemaps-0.9 warnings for a parsed document — the
// rules the protocol states but that real sitemaps commonly bend (a reader still
// uses the document). Empty when fully conformant. The byte-size limit is not
// checked here (it needs the source bytes); Lint adds it.
func Conformance(doc *xmlpb.Xml) []string {
	root := doc.GetRoot()
	if root == nil {
		return nil
	}
	var warn []string
	if ns := root.GetNamespace().GetNamespaceUri(); ns != NamespaceURI {
		if ns == "" {
			warn = append(warn, fmt.Sprintf("<%s> does not declare the sitemap namespace %q", localName(root), NamespaceURI))
		} else {
			warn = append(warn, fmt.Sprintf("<%s> namespace is %q, want %q", localName(root), ns, NamespaceURI))
		}
	}
	switch localName(root) {
	case "urlset":
		warn = append(warn, conformEntries(root, "url", true)...)
	case "sitemapindex":
		warn = append(warn, conformEntries(root, "sitemap", false)...)
	}
	return warn
}

// conformEntries checks every <url>/<sitemap> child of root: the entry count
// against the limit, then each entry's <loc>/<lastmod> (and, for <url>,
// <changefreq>/<priority>). urlExtras selects the url-only value rules.
func conformEntries(root *xmlpb.Tag, kind string, urlExtras bool) []string {
	var warn []string
	entries := childrenNamed(root, kind)
	if n := len(entries); n > MaxEntries {
		warn = append(warn, fmt.Sprintf("<%s> has %d <%s> entries, exceeds the %d limit", localName(root), n, kind, MaxEntries))
	}
	for i, e := range entries {
		locs := childrenNamed(e, "loc")
		switch len(locs) {
		case 0:
			warn = append(warn, fmt.Sprintf("<%s> #%d is missing the required <loc>", kind, i+1))
		case 1:
			warn = append(warn, checkLoc(locs[0], kind, i)...)
		default:
			warn = append(warn, fmt.Sprintf("<%s> #%d has %d <loc> elements, want exactly one", kind, i+1, len(locs)))
			warn = append(warn, checkLoc(locs[0], kind, i)...)
		}
		for _, lm := range childrenNamed(e, "lastmod") {
			if v := strings.TrimSpace(text(lm)); v != "" && !validW3CDatetime(v) {
				warn = append(warn, fmt.Sprintf("<%s> #%d <lastmod> %q is not a valid W3C Datetime", kind, i+1, v))
			}
		}
		if !urlExtras {
			continue
		}
		for _, cf := range childrenNamed(e, "changefreq") {
			if v := strings.TrimSpace(text(cf)); v != "" && !changefreqValues[v] {
				warn = append(warn, fmt.Sprintf("<url> #%d <changefreq> %q is not one of always|hourly|daily|weekly|monthly|yearly|never", i+1, v))
			}
		}
		for _, pr := range childrenNamed(e, "priority") {
			v := strings.TrimSpace(text(pr))
			if v == "" {
				continue
			}
			if !validPriority(v) {
				warn = append(warn, fmt.Sprintf("<url> #%d <priority> %q is not a number in [0.0, 1.0]", i+1, v))
			}
		}
	}
	return warn
}

// checkLoc validates a <loc> value: non-empty, at most MaxLocLen characters, and
// an absolute URL.
func checkLoc(loc *xmlpb.Tag, kind string, i int) []string {
	var warn []string
	v := strings.TrimSpace(text(loc))
	if v == "" {
		return append(warn, fmt.Sprintf("<%s> #%d has an empty <loc>", kind, i+1))
	}
	if len(v) > MaxLocLen {
		warn = append(warn, fmt.Sprintf("<%s> #%d <loc> is %d characters, exceeds the %d limit", kind, i+1, len(v), MaxLocLen))
	}
	// A sitemap <loc> must be a crawlable http/https URL. url.Parse alone accepts
	// any absolute-scheme URI (javascript:, data:, file:, mailto:, …), so the
	// scheme is checked explicitly.
	if u, err := url.Parse(v); err != nil || !u.IsAbs() || (u.Scheme != "http" && u.Scheme != "https") {
		warn = append(warn, fmt.Sprintf("<%s> #%d <loc> %q is not an http(s) URL", kind, i+1, v))
	}
	return warn
}

// validPriority reports whether v is a Sitemaps-0.9 <priority>: a plain decimal
// numeral whose value is in [0.0, 1.0]. The lexical check rejects the forms
// strconv.ParseFloat would otherwise accept but the protocol does not — NaN, Inf,
// exponents (1e0), hexadecimal floats (0x1p-1), and a leading sign — and the
// range check rejects out-of-band values. The accepted set is regular (ADR 0003).
func validPriority(v string) bool {
	if !priorityRe.MatchString(v) {
		return false
	}
	f, err := strconv.ParseFloat(v, 64)
	return err == nil && f >= 0.0 && f <= 1.0
}

// w3cLayouts are the levels of the W3C Datetime profile the protocol allows for
// <lastmod> (https://www.w3.org/TR/NOTE-datetime): a year, year-month, a
// complete date, or a date-time to the minute/second/fraction, each with a
// timezone. Go's "Z07:00" accepts both "Z" and "+hh:mm".
var w3cLayouts = []string{
	"2006",
	"2006-01",
	"2006-01-02",
	"2006-01-02T15:04Z07:00",
	"2006-01-02T15:04:05Z07:00",
	"2006-01-02T15:04:05.999999999Z07:00",
}

func validW3CDatetime(s string) bool {
	for _, l := range w3cLayouts {
		if _, err := time.Parse(l, s); err == nil {
			return true
		}
	}
	return false
}

// --- generic Tag-tree helpers (walk the exported proto accessors) ---

// URLCount and SitemapCount report the number of <url> / <sitemap> entries
// directly under the document root — a cheap structural summary over the
// generic tree, matched by local name.
func URLCount(doc *xmlpb.Xml) int     { return len(childrenNamed(doc.GetRoot(), "url")) }
func SitemapCount(doc *xmlpb.Xml) int { return len(childrenNamed(doc.GetRoot(), "sitemap")) }

// localName returns an element's namespace-local name — the resolved local part
// when namespaces were applied, else the part after any prefix. The sitemap core
// is matched by local name, so a document that declares the sitemap namespace
// and one that omits it are treated alike.
func localName(t *xmlpb.Tag) string {
	if ln := t.GetNamespace().GetLocalName(); ln != "" {
		return ln
	}
	name := t.GetName()
	if i := strings.IndexByte(name, ':'); i >= 0 {
		return name[i+1:]
	}
	return name
}

// childrenNamed returns t's child elements whose local name is name.
func childrenNamed(t *xmlpb.Tag, name string) []*xmlpb.Tag {
	var out []*xmlpb.Tag
	for _, ci := range t.GetContents() {
		if c := ci.GetChild(); c != nil && localName(c) == name {
			out = append(out, c)
		}
	}
	return out
}

// text returns the character content of a leaf element: all of its descendant
// text and CDATA, concatenated in document order.
func text(t *xmlpb.Tag) string {
	var b strings.Builder
	var walk func(*xmlpb.Tag)
	walk = func(n *xmlpb.Tag) {
		for _, ci := range n.GetContents() {
			switch it := ci.GetItem().(type) {
			case *xmlpb.ContentItem_Text:
				b.WriteString(it.Text)
			case *xmlpb.ContentItem_Cdata:
				b.WriteString(it.Cdata)
			case *xmlpb.ContentItem_Child:
				walk(it.Child)
			}
		}
	}
	walk(t)
	return b.String()
}
