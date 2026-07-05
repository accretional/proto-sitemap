#!/usr/bin/env bash
# setup.sh — make a clean checkout buildable with no manual steps: check out the
# local-module dependencies as sibling repos and download the Go modules.
# Idempotent: anything already present is skipped. Unlike xmile, proto-sitemap
# needs no protoc/codegen — the sitemap format is data (formats/sitemap.ebnf),
# compiled to a descriptor at runtime by xmile's engine.
set -euo pipefail
cd "$(dirname "$0")"

# 1. Go toolchain (required; cannot be auto-installed portably). Needs Go 1.26+.
command -v go >/dev/null || { echo "[setup] FATAL: install Go 1.26+ (https://go.dev/dl/) and re-run"; exit 1; }

# 2. Sibling dependency repos. go.mod pins them via `replace => ../<dep>`, so a
#    clean clone needs them checked out next to this repo. xmile's own replaces do
#    not carry over transitively, so gluon and proto-merge are cloned here too.
for dep in xmile gluon proto-merge; do
  if [ ! -d "../$dep/.git" ]; then
    echo "[setup] cloning $dep -> ../$dep"
    git clone --depth 1 "https://github.com/accretional/$dep" "../$dep"
  else
    echo "[setup] updating $dep -> latest"
    git -C "../$dep" fetch --quiet origin || true
    git -C "../$dep" pull --ff-only --quiet 2>/dev/null \
      || echo "[setup] WARN: $dep not fast-forwarded (diverged or local changes) — using current state"
  fi
done

go mod download
echo "[setup] OK"
