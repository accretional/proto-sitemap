#!/usr/bin/env bash
# LET_IT_RIP.sh — the full gate: set up, build, and test everything (unit tests +
# the real-sitemap corpus). Chains: LET_IT_RIP.sh -> test.sh -> build.sh -> setup.sh.
# A clean checkout needs nothing else installed but Go 1.26+.
set -euo pipefail
cd "$(dirname "$0")"
./test.sh

echo "[rip] OK"
