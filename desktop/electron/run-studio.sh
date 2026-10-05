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

echo "==> frontend"
(cd "$here/../frontend-next" && pnpm install --frozen-lockfile >/dev/null && pnpm build >/dev/null)

echo "==> kernel"
(cd "$here" && pnpm build:host)

echo "==> studio"
cd "$here" && exec pnpm start
