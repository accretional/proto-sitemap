package main

// expand.go — the walk: sitemap URLs in, page URLs out.
//
// A <sitemapindex> may list child sitemaps, so expansion is a breadth-first
// traversal, not a single parse. It is bounded on every axis (URLs, documents,
// depth), it never visits the same document twice (indexes pointing at each
// other are a real shape in the wild), and a failed child is recorded rather
// than fatal — a site serving HTML at /sitemap.xml is expected, not a bug.
//
// Levels are fetched concurrently and parsed serially: fetching is the slow part
// and parallelizes cleanly, while serial parsing keeps one xmile parser to one
// goroutine without relying on its concurrency guarantees.

import (
	"context"
	"fmt"
	"math"
	"net/url"
	"sync"
	"time"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	pb "github.com/accretional/proto-sitemap/proto/pb"
	sitemap "github.com/accretional/proto-sitemap/service"
)

// DefaultLimits are applied to any zero field of a request's Limits.
//
// All three default to MaxInt32 — that is, a walk is unbounded unless the
// CALLER bounds it. The protocol permits a sitemapindex of 50,000 sitemaps of
// 50,000 URLs each, so "unbounded" really is up to 2.5 billion URLs, and the
// budgets are no longer what stops that. What stops it instead:
//
//   - the request deadline (-request-timeout, 840s in the deployed config),
//   - the 64 MiB gRPC message cap, which a response exceeds at roughly 600k
//     URLs — and exceeding it FAILS the RPC with ResourceExhausted rather than
//     returning a truncated answer, so a caller gets nothing rather than a
//     prefix,
//   - instance memory: every URL found is held until the walk completes.
//
// Callers who want a bounded, always-answerable walk should set limits
// explicitly and read the truncated flag. See ADR/README notes.
var DefaultLimits = pb.Limits{
	MaxUrls:     math.MaxInt32,
	MaxSitemaps: math.MaxInt32,
	MaxDepth:    math.MaxInt32,
}

// withDefaults returns l with every zero field replaced by its default.
func withDefaults(l *pb.Limits) pb.Limits {
	out := DefaultLimits
	if l == nil {
		return out
	}
	if l.MaxUrls > 0 {
		out.MaxUrls = l.MaxUrls
	}
	if l.MaxSitemaps > 0 {
		out.MaxSitemaps = l.MaxSitemaps
	}
	if l.MaxDepth > 0 {
		out.MaxDepth = l.MaxDepth
	}
	return out
}

// expander walks sitemaps. Construct one per request.
type expander struct {
	fetch          *fetcher
	parser         parserFunc
	allowCrossHost bool
}

// parserFunc parses one document into its typed AST and root element name. It is
// a field so tests can drive the walk without an xmile parser.
type parserFunc func(src string) (proto.Message, string, error)

// node is one document queued for the walk.
type node struct {
	url   string
	depth int
}

// Expand walks from req's sitemap URLs and returns every reachable page URL.
func (e *expander) Expand(ctx context.Context, req *pb.ExpandRequest) *pb.ExpandResponse {
	start := time.Now()
	lim := withDefaults(req.GetLimits())

	resp := &pb.ExpandResponse{Urls: []*pb.UrlEntry{}}
	seen := map[string]bool{}
	queue := make([]node, 0, len(req.GetSitemapUrls()))
	for _, u := range req.GetSitemapUrls() {
		if u != "" && !seen[u] {
			seen[u] = true
			queue = append(queue, node{url: u, depth: 0})
		}
	}

	truncate := func(reason string) {
		if !resp.Truncated {
			resp.Truncated, resp.TruncationReason = true, reason
		}
	}

	for len(queue) > 0 && !resp.Truncated {
		level := queue
		queue = nil

		// Do not fetch more documents than the budget allows.
		if remaining := int(lim.MaxSitemaps - resp.SitemapsFetched); len(level) > remaining {
			level = level[:max(remaining, 0)]
			truncate(fmt.Sprintf("max_sitemaps (%d) reached", lim.MaxSitemaps))
		}
		if len(level) == 0 {
			break
		}

		for _, res := range e.fetchLevel(ctx, level) {
			if res.err != nil {
				resp.Errors = append(resp.Errors, &pb.FetchError{Url: res.node.url, Error: res.err.Error()})
				continue
			}
			resp.SitemapsFetched++

			msg, root, err := e.parser(string(res.body))
			if err != nil {
				resp.Errors = append(resp.Errors, &pb.FetchError{Url: res.node.url, Error: err.Error()})
				continue
			}

			switch root {
			case "urlset":
				for _, entry := range entries(msg, "url") {
					if len(resp.Urls) >= int(lim.MaxUrls) {
						truncate(fmt.Sprintf("max_urls (%d) reached", lim.MaxUrls))
						break
					}
					loc := leafText(entry, "loc")
					if loc == "" {
						continue
					}
					resp.Urls = append(resp.Urls, &pb.UrlEntry{
						Loc:           resolve(res.node.url, loc),
						Lastmod:       leafText(entry, "lastmod"),
						SourceSitemap: res.node.url,
					})
				}
			case "sitemapindex":
				if int32(res.node.depth+1) > lim.MaxDepth {
					truncate(fmt.Sprintf("max_depth (%d) reached", lim.MaxDepth))
					continue
				}
				for _, entry := range entries(msg, "sitemap") {
					loc := leafText(entry, "loc")
					if loc == "" {
						continue
					}
					child := resolve(res.node.url, loc)
					if seen[child] {
						continue
					}
					if !e.allowCrossHost && !sameHost(res.node.url, child) {
						resp.Errors = append(resp.Errors, &pb.FetchError{
							Url:   child,
							Error: "cross-host sitemap child refused (-allow-cross-host to permit)",
						})
						continue
					}
					seen[child] = true
					queue = append(queue, node{url: child, depth: res.node.depth + 1})
				}
			default:
				resp.Errors = append(resp.Errors, &pb.FetchError{
					Url:   res.node.url,
					Error: fmt.Sprintf("unexpected sitemap root %q", root),
				})
			}
		}
	}

	resp.ElapsedMs = time.Since(start).Milliseconds()
	return resp
}

// fetchResult pairs a queued node with what fetching it produced.
type fetchResult struct {
	node node
	body []byte
	err  error
}

// fetchLevel fetches one breadth-first level. Ordering of the results follows the
// queue, so a walk is deterministic given the same origin responses.
func (e *expander) fetchLevel(ctx context.Context, level []node) []fetchResult {
	out := make([]fetchResult, len(level))
	var wg sync.WaitGroup
	for i, n := range level {
		wg.Add(1)
		go func(i int, n node) {
			defer wg.Done()
			body, err := e.fetch.get(ctx, n.url)
			out[i] = fetchResult{node: n, body: body, err: err}
		}(i, n)
	}
	wg.Wait()
	return out
}

// resolve turns a possibly-relative <loc> into an absolute URL against the
// sitemap that carried it. The protocol requires <loc> to be absolute, but real
// generators emit relative values and the intent is unambiguous.
func resolve(base, loc string) string {
	b, err := url.Parse(base)
	if err != nil {
		return loc
	}
	u, err := url.Parse(loc)
	if err != nil {
		return loc
	}
	return b.ResolveReference(u).String()
}

// sameHost reports whether child is on the same host as its parent sitemap.
// Sitemaps may only list URLs under the host that serves them unless the site
// has been cross-verified, so a child on another host is refused by default —
// it is both the spec's rule and the shape an untrusted index would use to aim
// the fetcher somewhere it was never pointed.
func sameHost(parent, child string) bool {
	p, err := url.Parse(parent)
	if err != nil {
		return false
	}
	c, err := url.Parse(child)
	if err != nil {
		return false
	}
	return p.Host == c.Host
}

// xmileParser adapts the sitemap service to parserFunc, holding one parser for
// the life of the walk.
func xmileParser() (parserFunc, error) {
	p, err := sitemap.Parser()
	if err != nil {
		return nil, err
	}
	return func(src string) (proto.Message, string, error) {
		return sitemap.Process(p, src)
	}, nil
}

// entries returns the repeated `field` ("url" or "sitemap") of a typed sitemap AST.
func entries(msg proto.Message, field string) []protoreflect.Message {
	m := msg.ProtoReflect()
	fd := m.Descriptor().Fields().ByName(protoreflect.Name(field))
	if fd == nil || !fd.IsList() || fd.Kind() != protoreflect.MessageKind {
		return nil
	}
	list := m.Get(fd).List()
	out := make([]protoreflect.Message, 0, list.Len())
	for i := 0; i < list.Len(); i++ {
		out = append(out, list.Get(i).Message())
	}
	return out
}

// maxLeafSearchNodes bounds the search below one entry. An entry is a handful of
// nodes; the cap only matters for a pathological document.
const maxLeafSearchNodes = 512

// leafText finds the shallowest descendant message named `name` under entry and
// returns its text.
//
// The walk is by name rather than by a fixed path because the compiled
// descriptor interposes the grammar's own alternation messages — today a <loc>
// sits at url > alt1 > loc > text — and those are an artifact of how
// formats/sitemap.ebnf spells the vocabulary, not of the sitemap format. A
// breadth-first search by name survives a regrammar; a hardcoded "alt1" would
// break silently, yielding empty URLs rather than an error.
func leafText(entry protoreflect.Message, name string) string {
	target := protoreflect.Name(name)
	queue := []protoreflect.Message{entry}
	for visited := 0; len(queue) > 0 && visited < maxLeafSearchNodes; visited++ {
		cur := queue[0]
		queue = queue[1:]

		if fd := cur.Descriptor().Fields().ByName(target); fd != nil &&
			fd.Kind() == protoreflect.MessageKind && !fd.IsList() && cur.Has(fd) {
			return textOf(cur.Get(fd).Message())
		}

		cur.Range(func(fd protoreflect.FieldDescriptor, v protoreflect.Value) bool {
			if fd.Kind() != protoreflect.MessageKind {
				return true
			}
			if fd.IsList() {
				l := v.List()
				for i := 0; i < l.Len(); i++ {
					queue = append(queue, l.Get(i).Message())
				}
				return true
			}
			queue = append(queue, v.Message())
			return true
		})
	}
	return ""
}

// textOf reads the `text` field a leaf element message carries.
func textOf(m protoreflect.Message) string {
	fd := m.Descriptor().Fields().ByName("text")
	if fd == nil || fd.Kind() != protoreflect.StringKind || fd.IsList() {
		return ""
	}
	return m.Get(fd).String()
}
