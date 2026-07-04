package main

// fetch.go — the corpus fetcher. It downloads a curated set of real, publicly
// served sitemaps into a gitignored cache, gunzipping the compressed ones, so
// the corpus runner can parse and round-trip sitemaps as they occur in the wild.
// Fetching is best-effort: an unreachable URL is logged and skipped, and a
// cached file is never re-fetched, so re-runs work offline.

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const corpusDir = "testing/corpus/sitemaps"

// sitemapURLs is a curated set of real sitemaps — a mix of <urlset> and
// <sitemapindex>, plain and gzipped, from many different generators — chosen to
// exercise the parser and the round-trip against sitemaps in the wild. Any that
// is unreachable or has moved is skipped; the list is best-effort by design.
var sitemapURLs = []string{
	// <urlset> (a set of URLs)
	"https://www.sitemaps.org/sitemap.xml", // the protocol's own site
	"https://www.cloudflare.com/sitemap.xml",
	"https://www.gov.uk/sitemap.xml",
	"https://developer.mozilla.org/sitemap.xml",
	"https://www.smashingmagazine.com/sitemap.xml",
	"https://www.elastic.co/sitemap.xml",
	// <sitemapindex> (an index of child sitemaps)
	"https://kubernetes.io/sitemap.xml",
	"https://wordpress.org/sitemap.xml",
	"https://developers.google.com/sitemap.xml",
	"https://moz.com/sitemaps-1-sitemap.xml",
	"https://nodejs.org/sitemap.xml",
	"https://www.postgresql.org/sitemap.xml",
	"https://about.gitlab.com/sitemap.xml",
	"https://www.docker.com/sitemap.xml",
	// gzipped and/or news sitemaps
	"https://www.theguardian.com/sitemaps/news.xml",
	"https://www.bbc.com/sitemaps/https-index-com-news.xml",
}

// fetchCorpus downloads each sitemap into corpusDir (gunzipping .gz payloads),
// skipping any already cached, and returns the local paths present afterward.
func fetchCorpus() ([]string, error) {
	if err := os.MkdirAll(corpusDir, 0o755); err != nil {
		return nil, err
	}
	client := &http.Client{Timeout: 30 * time.Second}
	for _, u := range sitemapURLs {
		path := filepath.Join(corpusDir, sanitize(u))
		if _, err := os.Stat(path); err == nil {
			continue // cached
		}
		body, err := download(client, u)
		if err != nil {
			fmt.Printf("  [fetch] skip %s: %v\n", u, err)
			continue
		}
		if err := os.WriteFile(path, body, 0o644); err != nil {
			return nil, err
		}
		fmt.Printf("  [fetch] %s (%d bytes)\n", filepath.Base(path), len(body))
	}
	return listCorpus()
}

// download fetches u and returns its body, gunzipping when the payload is gzip
// (by magic bytes, covering both .gz URLs and gzip Content-Encoding).
func download(client *http.Client, u string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	// A browser-like UA: some CDNs reject the default Go user agent.
	req.Header.Set("User-Agent", "proto-sitemap-corpus/1.0 (+https://github.com/accretional/proto-sitemap)")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20)) // 64 MiB ceiling
	if err != nil {
		return nil, err
	}
	if len(body) >= 2 && body[0] == 0x1f && body[1] == 0x8b {
		zr, err := gzip.NewReader(bytes.NewReader(body))
		if err != nil {
			return nil, fmt.Errorf("gunzip: %w", err)
		}
		defer zr.Close()
		return io.ReadAll(io.LimitReader(zr, 128<<20))
	}
	return body, nil
}

// listCorpus returns the cached corpus file paths.
func listCorpus() ([]string, error) {
	entries, err := os.ReadDir(corpusDir)
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, e := range entries {
		if !e.IsDir() {
			paths = append(paths, filepath.Join(corpusDir, e.Name()))
		}
	}
	return paths, nil
}

// sanitize turns a URL into a stable, filesystem-safe cache filename ending in
// .xml.
func sanitize(u string) string {
	s := strings.TrimPrefix(strings.TrimPrefix(u, "https://"), "http://")
	s = strings.NewReplacer("/", "_", ":", "_", "?", "_", "&", "_", "=", "_").Replace(s)
	if !strings.HasSuffix(s, ".xml") {
		s += ".xml"
	}
	return s
}
