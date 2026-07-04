#!/usr/bin/env bash
# test.sh — build, then run the self-contained tests and the real-sitemap corpus
# checks. The corpus runner (go run ./testing) fetches a set of public sitemaps
# on first run, then gates round-trip + typed projection over them.
# Chains: test.sh -> build.sh -> setup.sh.
set -euo pipefail
cd "$(dirname "$0")"
./build.sh

echo "[test] go test ./..."
go test ./...

echo "[test] corpus checks (fetches real sitemaps on first run; gates round-trip + projection):"
go run ./testing

echo "[test] OK"
