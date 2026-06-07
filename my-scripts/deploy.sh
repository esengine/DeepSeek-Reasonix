#!/usr/bin/env bash
set -euo pipefail

# deploy.sh — Build reasonix Go binary and install it as reasonix-go.
#
# Usage:
#   ./deploy.sh                              # auto-detect and overwrite existing
#   ./deploy.sh /custom/path/reasonix-go     # custom path
#
# Auto-detects existing binary on PATH. Uses sudo if target dir is not writable.

cd "$(dirname "$0")/.." # project root

# Auto-detect existing reasonix-go on PATH
TARGET=""
if which reasonix-go &>/dev/null; then
  TARGET="$(which reasonix-go)"
fi

# If a path was given explicitly, use it
if [ $# -ge 1 ]; then
  TARGET="$1"
fi

# If no explicit target and no existing binary, pick a reasonable default
if [ -z "$TARGET" ]; then
  TARGET="/usr/local/bin/reasonix-go"
fi

TARGET_DIR="$(dirname "${TARGET}")"

# Build
VERSION="$(git describe --tags --always 2>/dev/null || echo dev)-wkj"
LDFLAGS="-s -w -X main.version=${VERSION}"

BUILD_DIR="$(mktemp -d)"
trap 'rm -rf "${BUILD_DIR}"' EXIT

echo "▸ Building reasonix ${VERSION}"
CGO_ENABLED=0 go build -ldflags "${LDFLAGS}" -o "${BUILD_DIR}/reasonix-go" ./cmd/reasonix

echo "▸ Installing → ${TARGET}"
mkdir -p "${TARGET_DIR}"
if [ ! -w "${TARGET_DIR}" ]; then
  sudo cp -f "${BUILD_DIR}/reasonix-go" "${TARGET}"
  sudo chmod +x "${TARGET}"
else
  cp -f "${BUILD_DIR}/reasonix-go" "${TARGET}"
  chmod +x "${TARGET}"
fi

echo "✓ Deployed: $(file "${TARGET}")"
echo "             $(ls -lh "${TARGET}" | awk '{print $5}')"
echo ""
echo "▸ Run: $(basename "${TARGET}") chat"
