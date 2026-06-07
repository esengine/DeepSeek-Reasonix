#!/usr/bin/env bash
set -euo pipefail

# build.sh — Compile reasonix Go binary for the current platform.
#
# Usage: ./build.sh [output-path]
#   default output-path: ../bin/reasonix-go (relative to this script)

cd "$(dirname "$0")/.." # project root
OUTPUT="${1:-bin/reasonix-go}"

VERSION="$(git describe --tags --always 2>/dev/null || echo dev)-wkj"
LDFLAGS="-s -w -X main.version=${VERSION}"

echo "▸ Building reasonix ${VERSION} → ${OUTPUT}"
CGO_ENABLED=0 go build -ldflags "${LDFLAGS}" -o "${OUTPUT}" ./cmd/reasonix
echo "✓ Done: $(file "${OUTPUT}")"
