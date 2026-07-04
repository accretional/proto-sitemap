#!/usr/bin/env bash
# build.sh — set up dependencies and build, ready to test. The sitemap format is
# data compiled at runtime, so there is no codegen step. Chains: build.sh -> setup.sh.
set -euo pipefail
cd "$(dirname "$0")"
./setup.sh

echo "[build] go build ./..."
go build ./...
go mod tidy >/dev/null 2>&1 || true
echo "[build] OK"
