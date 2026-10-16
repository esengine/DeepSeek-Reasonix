#!/usr/bin/env bash
#
# Build the interface and copy it where the APK will carry it.
#
# The page that ships is the same build the desktop Studio serves — that is the
# whole reason the mobile layout lives in `desktop/frontend-next` rather than in
# an Android project's own web root. Nothing is edited on the way through: the
# only thing this script does that `pnpm build` does not is copy `dist/` into the
# app's assets.
#
# Output: app/src/main/assets/page/
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
android="$here/.."
frontend="$android/../upstream/desktop/frontend-next"
dest="$android/app/src/main/assets/page"

if [ ! -d "$frontend" ]; then
  echo "找不到前端工程：$frontend" >&2
  exit 1
fi

cd "$frontend"

# pnpm is the package manager the lockfile is written for; npm would resolve a
# different tree than the one that was tested.
if ! command -v pnpm >/dev/null 2>&1; then
  echo "需要 pnpm 10：npm install -g pnpm@10" >&2
  exit 1
fi

if [ ! -d node_modules ]; then
  echo "正在安装前端依赖…"
  pnpm install --frozen-lockfile
fi

echo "正在构建前端…"
pnpm build

rm -rf "$dest"
mkdir -p "$dest"
cp -R dist/. "$dest/"

if [ ! -f "$dest/index.html" ]; then
  echo "构建产物里没有 index.html，前端构建可能失败了。" >&2
  exit 1
fi

count=$(find "$dest" -type f | wc -l)
size=$(du -sk "$dest" | cut -f1)
echo "页面就绪：$dest（$count 个文件，$((size / 1024)) MiB）"
