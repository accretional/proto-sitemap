#!/usr/bin/env bash
# regen.sh — regenerate the Go bindings for proto/sitemap_service.proto.
#
# This repo generates and commits no proto for the sitemap FORMAT: that is data
# (formats/sitemap.ebnf), compiled to a descriptor at runtime (CLAUDE.md,
# ADR 0001). The one .proto here describes the SERVICE, and CLAUDE.md's rule for
# adding a hand-written proto is what this script satisfies.
#
# Sources live in proto/, generated Go in proto/pb/ — the layout xmile uses.
# The generated .pb.go files are COMMITTED, so an ordinary build needs no
# protoc; only editing the .proto does.
set -euo pipefail
cd "$(dirname "$0")"

for t in protoc protoc-gen-go protoc-gen-go-grpc; do
  command -v "$t" >/dev/null 2>&1 || {
    echo "[regen] FATAL: $t not found." >&2
    echo "  brew install protobuf" >&2
    echo "  go install google.golang.org/protobuf/cmd/protoc-gen-go@latest" >&2
    echo "  go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest" >&2
    exit 1
  }
done

echo "[regen] proto/sitemap_service.proto"
mkdir -p proto/pb
protoc \
  --proto_path=proto \
  --go_out=proto/pb --go_opt=paths=source_relative \
  --go-grpc_out=proto/pb --go-grpc_opt=paths=source_relative \
  proto/sitemap_service.proto

gofmt -w proto/pb
echo "[regen] OK — commit the regenerated .pb.go files"
