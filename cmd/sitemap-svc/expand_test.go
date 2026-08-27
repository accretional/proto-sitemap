package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func urlset(locs ...string) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` +
		`<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">`)
	for _, l := range locs {
		fmt.Fprintf(&b, `<url><loc>%s</loc><lastmod>2026-01-01</lastmod></url>`, l)
	}
	b.WriteString(`</urlset>`)
	return b.String()
}

func sitemapindex(locs ...string) string {
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>` +
		`<sitemapindex xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">`)
	for _, l := range locs {
		fmt.Fprintf(&b, `<sitemap><loc>%s</loc></sitemap>`, l)
	}
	b.WriteString(`</sitemapindex>`)
	return b.String()
}

func gz(t *testing.T, s string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write([]byte(s)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// newExpander wires a real xmile parse over a test server.
func newExpander(t *testing.T, delay time.Duration, crossHost bool) *expander {
	t.Helper()
	parse, err := xmileParser()
	if err != nil {
		t.Fatalf("parser: %v", err)
	}
	return &expander{
		fetch:          newFetcher("test", delay, 5*time.Second, 8),
		parser:         parse,
		allowCrossHost: crossHost,
	}
}

// serve returns a test server dispatching by path, and its base URL.
func serve(t *testing.T, routes map[string]any) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, ok := routes[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		switch v := body.(type) {
		case string:
			w.Write([]byte(v))
		case []byte:
			w.Write(v)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func locs(resp ExpandResponse) []string {
	out := make([]string, 0, len(resp.URLs))
	for _, u := range resp.URLs {
		out = append(out, u.Loc)
	}
	return out
}

func TestExpand_Urlset(t *testing.T) {
	srv := serve(t, map[string]any{
		"/sitemap.xml": urlset("https://example.com/a", "https://example.com/b"),
	})
	e := newExpander(t, 0, false)
	resp := e.Expand(context.Background(), ExpandRequest{SitemapURLs: []string{srv.URL + "/sitemap.xml"}})

	if got := locs(resp); len(got) != 2 || got[0] != "https://example.com/a" {
		t.Fatalf("urls = %v, want the two locs", got)
	}
	if resp.URLs[0].Lastmod != "2026-01-01" {
		t.Errorf("lastmod = %q, want 2026-01-01", resp.URLs[0].Lastmod)
	}
	if resp.SitemapsFetched != 1 || resp.Truncated {
		t.Errorf("fetched=%d truncated=%v, want 1/false", resp.SitemapsFetched, resp.Truncated)
	}
}

// An index is followed to its children, and a .gz child is gunzipped.
func TestExpand_IndexRecursionAndGzip(t *testing.T) {
	var srv *httptest.Server
	srv = serve(t, map[string]any{})
	routes := map[string]any{
		"/sitemap.xml": "",
		"/a.xml":       urlset("https://example.com/a"),
		"/b.xml.gz":    gz(t, urlset("https://example.com/b")),
	}
	srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/sitemap.xml" {
			w.Write([]byte(sitemapindex(srv.URL+"/a.xml", srv.URL+"/b.xml.gz")))
			return
		}
		switch v := routes[r.URL.Path].(type) {
		case string:
			w.Write([]byte(v))
		case []byte:
			w.Write(v)
		default:
			http.NotFound(w, r)
		}
	})

	e := newExpander(t, 0, false)
	resp := e.Expand(context.Background(), ExpandRequest{SitemapURLs: []string{srv.URL + "/sitemap.xml"}})

	got := locs(resp)
	if len(got) != 2 {
		t.Fatalf("urls = %v (errors %v), want 2", got, resp.Errors)
	}
	if resp.SitemapsFetched != 3 {
		t.Errorf("fetched = %d, want 3 (index + 2 children)", resp.SitemapsFetched)
	}
}

// An index that points at itself must terminate.
func TestExpand_CycleTerminates(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(sitemapindex(srv.URL + "/sitemap.xml")))
	}))
	t.Cleanup(srv.Close)

	e := newExpander(t, 0, false)
	done := make(chan ExpandResponse, 1)
	go func() {
		done <- e.Expand(context.Background(), ExpandRequest{SitemapURLs: []string{srv.URL + "/sitemap.xml"}})
	}()
	select {
	case resp := <-done:
		if resp.SitemapsFetched != 1 {
			t.Errorf("fetched = %d, want 1 (the self-reference is not revisited)", resp.SitemapsFetched)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("self-referencing index did not terminate")
	}
}

func TestExpand_MaxURLsTruncates(t *testing.T) {
	many := make([]string, 50)
	for i := range many {
		many[i] = fmt.Sprintf("https://example.com/%d", i)
	}
	srv := serve(t, map[string]any{"/sitemap.xml": urlset(many...)})

	e := newExpander(t, 0, false)
	resp := e.Expand(context.Background(), ExpandRequest{
		SitemapURLs: []string{srv.URL + "/sitemap.xml"},
		Limits:      &Limits{MaxURLs: 10},
	})
	if len(resp.URLs) != 10 || !resp.Truncated {
		t.Fatalf("urls=%d truncated=%v, want 10/true", len(resp.URLs), resp.Truncated)
	}
	if !strings.Contains(resp.TruncationReason, "max_urls") {
		t.Errorf("reason = %q, want it to name max_urls", resp.TruncationReason)
	}
}

func TestExpand_MaxDepthTruncates(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Every document is an index pointing one level deeper.
		w.Write([]byte(sitemapindex(fmt.Sprintf("%s%s/deeper", srv.URL, r.URL.Path))))
	}))
	t.Cleanup(srv.Close)

	e := newExpander(t, 0, false)
	resp := e.Expand(context.Background(), ExpandRequest{
		SitemapURLs: []string{srv.URL + "/s"},
		Limits:      &Limits{MaxDepth: 2},
	})
	if !resp.Truncated || !strings.Contains(resp.TruncationReason, "max_depth") {
		t.Fatalf("truncated=%v reason=%q, want a max_depth truncation", resp.Truncated, resp.TruncationReason)
	}
}

func TestExpand_MaxSitemapsTruncates(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/sitemap.xml" {
			children := make([]string, 20)
			for i := range children {
				children[i] = fmt.Sprintf("%s/c%d.xml", srv.URL, i)
			}
			w.Write([]byte(sitemapindex(children...)))
			return
		}
		w.Write([]byte(urlset("https://example.com" + r.URL.Path)))
	}))
	t.Cleanup(srv.Close)

	e := newExpander(t, 0, false)
	resp := e.Expand(context.Background(), ExpandRequest{
		SitemapURLs: []string{srv.URL + "/sitemap.xml"},
		Limits:      &Limits{MaxSitemaps: 5},
	})
	if !resp.Truncated || !strings.Contains(resp.TruncationReason, "max_sitemaps") {
		t.Fatalf("truncated=%v reason=%q, want a max_sitemaps truncation", resp.Truncated, resp.TruncationReason)
	}
	if resp.SitemapsFetched > 5 {
		t.Errorf("fetched = %d, want no more than the 5-document budget", resp.SitemapsFetched)
	}
}

// A cross-host child is refused by default and recorded, not followed.
func TestExpand_CrossHostRefusedByDefault(t *testing.T) {
	other := serve(t, map[string]any{"/o.xml": urlset("https://example.com/other")})
	srv := serve(t, map[string]any{"/sitemap.xml": sitemapindex(other.URL + "/o.xml")})

	e := newExpander(t, 0, false)
	resp := e.Expand(context.Background(), ExpandRequest{SitemapURLs: []string{srv.URL + "/sitemap.xml"}})
	if len(resp.URLs) != 0 {
		t.Errorf("urls = %v, want none (cross-host child refused)", locs(resp))
	}
	if len(resp.Errors) != 1 || !strings.Contains(resp.Errors[0].Error, "cross-host") {
		t.Errorf("errors = %v, want one cross-host refusal", resp.Errors)
	}

	// ...and followed when explicitly allowed.
	e = newExpander(t, 0, true)
	resp = e.Expand(context.Background(), ExpandRequest{SitemapURLs: []string{srv.URL + "/sitemap.xml"}})
	if len(resp.URLs) != 1 {
		t.Errorf("urls = %v with -allow-cross-host, want 1", locs(resp))
	}
}

// A dead or non-sitemap child is recorded; healthy siblings still yield URLs.
func TestExpand_BadChildIsRecordedNotFatal(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/sitemap.xml":
			w.Write([]byte(sitemapindex(srv.URL+"/good.xml", srv.URL+"/html.xml", srv.URL+"/404.xml")))
		case "/good.xml":
			w.Write([]byte(urlset("https://example.com/good")))
		case "/html.xml":
			w.Write([]byte("<html><body>not a sitemap</body></html>"))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	e := newExpander(t, 0, false)
	resp := e.Expand(context.Background(), ExpandRequest{SitemapURLs: []string{srv.URL + "/sitemap.xml"}})
	if got := locs(resp); len(got) != 1 || got[0] != "https://example.com/good" {
		t.Fatalf("urls = %v, want just the good child", got)
	}
	if len(resp.Errors) != 2 {
		t.Errorf("errors = %v, want 2 (non-sitemap + 404)", resp.Errors)
	}
}

// A crawl delay spaces successive fetches to the same host.
func TestExpand_HonoursCrawlDelay(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/sitemap.xml" {
			w.Write([]byte(sitemapindex(srv.URL+"/a.xml", srv.URL+"/b.xml")))
			return
		}
		w.Write([]byte(urlset("https://example.com" + r.URL.Path)))
	}))
	t.Cleanup(srv.Close)

	const delay = 150 * time.Millisecond
	e := newExpander(t, delay, false)
	start := time.Now()
	resp := e.Expand(context.Background(), ExpandRequest{
		SitemapURLs:       []string{srv.URL + "/sitemap.xml"},
		CrawlDelaySeconds: delay.Seconds(),
	})
	elapsed := time.Since(start)

	if len(resp.URLs) != 2 {
		t.Fatalf("urls = %v, want 2", locs(resp))
	}
	// Three same-host fetches with a delay between them: at least 2 gaps.
	if min := 2 * delay; elapsed < min {
		t.Errorf("walk took %v, want at least %v under a %v crawl delay", elapsed, min, delay)
	}
}

// Relative <loc> values resolve against the sitemap that carried them.
func TestExpand_ResolvesRelativeLoc(t *testing.T) {
	srv := serve(t, map[string]any{"/dir/sitemap.xml": urlset("/page", "sub/page2")})
	e := newExpander(t, 0, false)
	resp := e.Expand(context.Background(), ExpandRequest{SitemapURLs: []string{srv.URL + "/dir/sitemap.xml"}})

	want := []string{srv.URL + "/page", srv.URL + "/dir/sub/page2"}
	got := locs(resp)
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("urls = %v, want %v", got, want)
	}
}
