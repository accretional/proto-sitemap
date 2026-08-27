package main

// fetch.go — the HTTP side of the walk.
//
// Sitemaps come from arbitrary hosts, so every fetch is bounded: a response size
// ceiling, a decompressed-size ceiling (a gzip bomb is otherwise a trivial
// memory exhaustion), a per-request timeout, and a redirect cap. Crawl-delay,
// when the caller passes one through from robots.txt, is honoured by serializing
// that host's fetches and spacing them.
//
// Gzip handling is required, not optional: sitemaps are very commonly served as
// .xml.gz. The magic-byte test (rather than trusting the URL suffix or
// Content-Encoding) is the approach testing/fetch.go already uses for the corpus.

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"

	sitemap "github.com/accretional/proto-sitemap/service"
)

// DefaultUserAgent identifies this service to origins. A fetcher that hits real
// sites should say what it is and where to complain.
const DefaultUserAgent = "proto-sitemap-svc/1.0 (+https://github.com/accretional/proto-sitemap)"

const (
	// maxWireBytes caps the compressed bytes read from an origin. It is above
	// MaxInputBytes so an over-limit document is refused by the parser's own
	// boundary with a clear error rather than truncated into a parse failure.
	maxWireBytes = 64 << 20
	// maxDecompressedBytes caps gunzip output at one byte past the protocol
	// maximum: enough for guardSource to reject an oversized document, and a hard
	// bound on what a gzip bomb can allocate.
	maxDecompressedBytes = sitemap.MaxInputBytes + 1
)

// fetcher performs bounded HTTP GETs, pacing per host when a crawl delay applies.
type fetcher struct {
	client     *http.Client
	userAgent  string
	crawlDelay time.Duration

	sem chan struct{} // bounds concurrent fetches when no crawl delay applies

	mu    sync.Mutex
	hosts map[string]*hostState
}

// hostState serializes and spaces fetches to one host under a crawl delay.
type hostState struct {
	mu   sync.Mutex
	last time.Time
}

func newFetcher(userAgent string, crawlDelay time.Duration, timeout time.Duration, concurrency int) *fetcher {
	if userAgent == "" {
		userAgent = DefaultUserAgent
	}
	if concurrency < 1 {
		concurrency = 1
	}
	return &fetcher{
		client: &http.Client{
			Timeout: timeout,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 5 {
					return errors.New("stopped after 5 redirects")
				}
				return nil
			},
		},
		userAgent:  userAgent,
		crawlDelay: crawlDelay,
		sem:        make(chan struct{}, concurrency),
		hosts:      map[string]*hostState{},
	}
}

func (f *fetcher) hostState(host string) *hostState {
	f.mu.Lock()
	defer f.mu.Unlock()
	hs, ok := f.hosts[host]
	if !ok {
		hs = &hostState{}
		f.hosts[host] = hs
	}
	return hs
}

// get fetches raw, gunzipping a gzip payload. It honours the crawl delay: with
// one set, a host's fetches are serialized and spaced by at least that interval;
// without one, fetches run concurrently up to the fetcher's bound.
func (f *fetcher) get(ctx context.Context, rawURL string) ([]byte, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("parse url: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return nil, fmt.Errorf("unsupported scheme %q", u.Scheme)
	}

	if f.crawlDelay > 0 {
		hs := f.hostState(u.Host)
		hs.mu.Lock()
		defer hs.mu.Unlock()
		if wait := time.Until(hs.last.Add(f.crawlDelay)); wait > 0 {
			select {
			case <-time.After(wait):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		defer func() { hs.last = time.Now() }()
	} else {
		select {
		case f.sem <- struct{}{}:
			defer func() { <-f.sem }()
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", f.userAgent)
	req.Header.Set("Accept", "application/xml, text/xml, application/gzip;q=0.9, */*;q=0.8")

	resp, err := f.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxWireBytes))
	if err != nil {
		return nil, err
	}
	return gunzipIfNeeded(body)
}

// gunzipIfNeeded decompresses body when it is gzip, detected by magic bytes so
// that both .gz URLs and gzip Content-Encoding are covered regardless of how the
// origin labelled the response. Output is capped: a small archive can otherwise
// expand without bound.
func gunzipIfNeeded(body []byte) ([]byte, error) {
	if len(body) < 2 || body[0] != 0x1f || body[1] != 0x8b {
		return body, nil
	}
	zr, err := gzip.NewReader(bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("gunzip: %w", err)
	}
	defer zr.Close()
	out, err := io.ReadAll(io.LimitReader(zr, maxDecompressedBytes))
	if err != nil {
		return nil, fmt.Errorf("gunzip: %w", err)
	}
	return out, nil
}
