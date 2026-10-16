#!/usr/bin/env bash
#
# Build the Reasonix kernel for Android and place it where the APK packager
# expects a native library.
#
# Why a shell script and not a Gradle task: the kernel is a Go module that lives
# outside this Gradle build, with its own toolchain version pinned in go.mod. A
# Gradle task that invoked `go build` would make the Android build depend on a Go
# toolchain being configured for it, and would rebuild a 21MB binary on every
# `assembleDebug` even when nothing in the kernel changed. This runs when you
# mean it to, and the APK task only checks that the result is there.
#
# Output: app/src/main/jniLibs/arm64-v8a/libreasonix.so
#   The name is not cosmetic. Android extracts files under `jniLibs/` to
#   `nativeLibraryDir` and permits execution *there*; a binary shipped in
#   `assets/` would have to be copied to app storage and chmod'd, which fails on
#   a device whose data partition is mounted noexec. `lib*.so` is the shape the
#   packager recognises as a native library, so the kernel wears it.
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
android="$here/.."
upstream="$android/../upstream"
out="$android/app/src/main/jniLibs/arm64-v8a/libreasonix.so"

if [ ! -d "$upstream" ]; then
  echo "找不到内核源码：$upstream" >&2
  echo "请先把 DeepSeek-Reasonix 仓库（studio 分支）放到 $upstream。" >&2
  exit 1
fi

# GOOS=android needs nothing else: the kernel is CGO-free, which is what makes a
# single static binary possible in the first place. `-s -w` drops the symbol and
# DWARF tables — about a third of the file — and nothing on a phone reads them.
export GOOS=android
export GOARCH=arm64
export CGO_ENABLED=0

# The app version, read from the module's own build.gradle so the two cannot
# drift. The official build stamps these through `-X` (see upstream's Makefile);
# without them the kernel reports `version=dev revision=unknown`, which a phone
# then shows next to the documentation it was built with and makes a bug report
# from that build impossible to place. There is no `.git` here, so the revision
# names the package the corpus shipped in rather than a commit hash — the honest
# answer when there is no commit to name.
app_gradle="$android/app/build.gradle"
version_field() {
  sed -n "s/^def $1 *= *\([0-9]*\).*/\1/p" "$app_gradle" | head -n1
}
app_version="$(version_field versionMajor).$(version_field versionMinor).$(version_field versionPatch)"
build_time_utc="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
ldflags="-s -w"
ldflags="$ldflags -X main.version=v$app_version"
ldflags="$ldflags -X main.buildTimeUTC=$build_time_utc"
ldflags="$ldflags -X reasonix/internal/tools/productdocs.linkedVersion=v$app_version"
ldflags="$ldflags -X reasonix/internal/tools/productdocs.linkedRevision=apk-v$app_version"

echo "正在编译内核（android/arm64，v$app_version）…"
mkdir -p "$(dirname "$out")"
cd "$upstream"
go build -trimpath -ldflags="$ldflags" -o "$out" ./cmd/reasonix

size=$(wc -c < "$out")
echo "内核就绪：$out（$((size / 1024 / 1024)) MiB）"
