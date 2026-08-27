// Command sitemap-svc serves proto-sitemap over HTTP/JSON: it walks the sitemap
// URLs it is given — following <sitemapindex> documents to their children — and
// returns every page URL it can reach.
//
// It is the fetching half of the library: proto-sitemap itself takes bytes, so
// this binary owns the HTTP concerns the format does not (gzip payloads, size
// and depth budgets, redirect and crawl-delay policy) and hands the bytes to
// service.Process for the parse.
//
//	POST /v1/sitemap:expand   {"sitemap_urls":[...]} -> {"urls":[...], ...}
//	GET  /healthz
//
// Flags are overridden by PORT (Cloud Run's contract) when that is set.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"time"
)

func main() {
	addr := flag.String("addr", ":8080", "listen address (PORT overrides the port)")
	fetchTimeout := flag.Duration("fetch-timeout", 30*time.Second, "per-sitemap HTTP timeout")
	requestTimeout := flag.Duration("request-timeout", 5*time.Minute, "whole-walk deadline")
	concurrency := flag.Int("concurrency", 8, "concurrent sitemap fetches (ignored under a crawl delay, which serializes per host)")
	allowCrossHost := flag.Bool("allow-cross-host", false, "follow sitemapindex children on other hosts")
	flag.Parse()

	if port := os.Getenv("PORT"); port != "" {
		*addr = ":" + port
	}

	// Compile the schema once at startup rather than on the first request: it is
	// the expensive step (EBNF -> descriptor) and a cold request should not pay it.
	if _, err := xmileParser(); err != nil {
		log.Fatalf("sitemap-svc: compile schema: %v", err)
	}

	s := &server{
		fetchTimeout:   *fetchTimeout,
		requestTimeout: *requestTimeout,
		concurrency:    *concurrency,
		allowCrossHost: *allowCrossHost,
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/sitemap:expand", s.expand)
	mux.HandleFunc("GET /healthz", s.healthz)

	srv := &http.Server{
		Addr:              *addr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	log.Printf("sitemap-svc: listening on %s", *addr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("sitemap-svc: %v", err)
	}
}

type server struct {
	fetchTimeout   time.Duration
	requestTimeout time.Duration
	concurrency    int
	allowCrossHost bool
}

func (s *server) expand(w http.ResponseWriter, r *http.Request) {
	var req ExpandRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body: "+err.Error())
		return
	}
	if len(req.SitemapURLs) == 0 {
		writeError(w, http.StatusBadRequest, "sitemap_urls must not be empty")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), s.requestTimeout)
	defer cancel()

	parse, err := xmileParser()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "compile schema: "+err.Error())
		return
	}

	delay := time.Duration(req.CrawlDelaySeconds * float64(time.Second))
	e := &expander{
		fetch:          newFetcher(req.UserAgent, delay, s.fetchTimeout, s.concurrency),
		parser:         parse,
		allowCrossHost: s.allowCrossHost,
	}

	resp := e.Expand(ctx, req)
	log.Printf("expand: roots=%d urls=%d sitemaps=%d errors=%d truncated=%v %dms",
		len(req.SitemapURLs), len(resp.URLs), resp.SitemapsFetched, len(resp.Errors), resp.Truncated, resp.ElapsedMS)
	writeJSON(w, http.StatusOK, resp)
}

func (s *server) healthz(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("write response: %v", err)
	}
}

func writeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}
