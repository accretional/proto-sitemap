// Command sitemap-svc serves proto-sitemap over gRPC: it walks the sitemap URLs
// it is given — following <sitemapindex> documents to their children — and
// returns every page URL it can reach.
//
//	sitemap.svc.v1.SitemapService/Expand
//
// Server reflection is registered, so grpcurl needs no .proto to call it, and
// the standard grpc.health.v1.Health service answers health checks.
//
// It is the fetching half of the library: proto-sitemap itself takes bytes, so
// this binary owns the HTTP concerns the format does not (gzip payloads, size
// and depth budgets, redirect and crawl-delay policy) and hands the bytes to
// service.Process for the parse.
package main

import (
	"context"
	"flag"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"

	pb "github.com/accretional/proto-sitemap/proto/pb"
)

// maxMessageBytes is generous because a walk's response carries every URL it
// found: 50,000 entries is several MiB, well past gRPC's 4 MiB default, and the
// failure mode would be an opaque ResourceExhausted at the transport layer.
const maxMessageBytes = 64 << 20

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

	lis, err := net.Listen("tcp", *addr)
	if err != nil {
		log.Fatalf("sitemap-svc: listen: %v", err)
	}

	srv := grpc.NewServer(
		grpc.MaxRecvMsgSize(maxMessageBytes),
		grpc.MaxSendMsgSize(maxMessageBytes),
	)
	pb.RegisterSitemapServiceServer(srv, &server{
		fetchTimeout:   *fetchTimeout,
		requestTimeout: *requestTimeout,
		concurrency:    *concurrency,
		allowCrossHost: *allowCrossHost,
	})

	hs := health.NewServer()
	hs.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)
	healthpb.RegisterHealthServer(srv, hs)
	reflection.Register(srv)

	// Drain in-flight walks on SIGTERM — Cloud Run sends one before shutdown,
	// and a walk can be minutes long.
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-stop
		log.Printf("sitemap-svc: shutting down")
		hs.SetServingStatus("", healthpb.HealthCheckResponse_NOT_SERVING)
		srv.GracefulStop()
	}()

	log.Printf("sitemap-svc: serving gRPC on %s", *addr)
	if err := srv.Serve(lis); err != nil {
		log.Fatalf("sitemap-svc: %v", err)
	}
}

type server struct {
	pb.UnimplementedSitemapServiceServer
	fetchTimeout   time.Duration
	requestTimeout time.Duration
	concurrency    int
	allowCrossHost bool
}

func (s *server) Expand(ctx context.Context, req *pb.ExpandRequest) (*pb.ExpandResponse, error) {
	if len(req.GetSitemapUrls()) == 0 {
		return nil, status.Error(codes.InvalidArgument, "sitemap_urls must not be empty")
	}

	ctx, cancel := context.WithTimeout(ctx, s.requestTimeout)
	defer cancel()

	parse, err := xmileParser()
	if err != nil {
		return nil, status.Errorf(codes.Internal, "compile schema: %v", err)
	}

	delay := time.Duration(req.GetCrawlDelaySeconds() * float64(time.Second))
	e := &expander{
		fetch:          newFetcher(req.GetUserAgent(), delay, s.fetchTimeout, s.concurrency),
		parser:         parse,
		allowCrossHost: s.allowCrossHost,
	}

	resp := e.Expand(ctx, req)
	log.Printf("Expand: roots=%d urls=%d sitemaps=%d errors=%d truncated=%v %dms",
		len(req.GetSitemapUrls()), len(resp.Urls), resp.SitemapsFetched, len(resp.Errors), resp.Truncated, resp.ElapsedMs)
	return resp, nil
}
