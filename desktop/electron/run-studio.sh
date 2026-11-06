#!/usr/bin/env bash
# Build Studio and launch it: the SPA, the kernel the shell speaks to over
# loopback, then the window. Unlike a bare Go binary this needs no bundling on
# macOS — Electron's own dev build is already an .app, so the native panels
# ("Add a folder…") that LaunchServices only gives a bundled process work here.
set -euo pipefail

here=$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)

if [ ! -d "$here/node_modules" ]; then
  echo "==> deps"
  (cd "$here" && pnpm install --frozen-lockfile)
fi

# A step is skipped when nothing it reads is newer than what it produced;
# STUDIO_REBUILD=1 forces every step.
stale() {
  [ -n "${STUDIO_REBUILD:-}" ] || node "$here/packaging/stale.js" "$@"
}

root="$here/../.."
web="$here/../frontend-next"
host=$(ls "$here"/bin/reasonix-studio-host* 2>/dev/null | head -n 1 || true)

if stale "$web/dist/index.html" "$web/src" "$web/public" "$web/index.html" "$web/package.json" "$web/pnpm-lock.yaml" "$web/vite.config.ts" "$web/tsconfig.json"; then
  echo "==> frontend"
  (cd "$web" && pnpm install --frozen-lockfile >/dev/null && pnpm build >/dev/null)
else
  echo "==> frontend (up to date)"
fi

if [ -z "$host" ] || stale "$host" "$root/internal" "$root/cmd" "$root/go.mod" "$root/go.sum" "$here/packaging"; then
  echo "==> kernel"
  (cd "$here" && pnpm build:host)
else
  echo "==> kernel (up to date)"
fi

echo "==> studio"
cd "$here" && exec pnpm start
