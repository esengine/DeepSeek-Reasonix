#!/usr/bin/env bash
# Build and package the Wails desktop app for one platform. Wails cannot
# cross-compile a CGO+webview binary, so this runs on a native runner per target
# (see .github/workflows/release-desktop.yml) and is invoked once per matrix entry.
#
# Output lands in <repo>/dist/ with stable, platform-keyed names that
# desktop/cmd/sign's `manifest` subcommand maps back to update.PlatformKey:
#   macOS:   Reasonix-darwin-<arch>.zip                  (ditto archive of the .app)
#   Windows: Reasonix-windows-<arch>-installer.exe       (NSIS per-user installer)
#   Linux:   Reasonix-linux-<arch>.tar.gz                (bare binary)
#
# Usage: scripts/desktop-build.sh <os/arch> <version>
#   e.g. scripts/desktop-build.sh darwin/arm64 v1.1.0
set -euo pipefail

PLATFORM="${1:?usage: desktop-build.sh <os/arch> <version>}"
VERSION="${2:?usage: desktop-build.sh <os/arch> <version>}"

os="${PLATFORM%/*}"
arch="${PLATFORM#*/}"

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
APPNAME="Reasonix"            # wails.json productName -> Reasonix.app
BINNAME="reasonix-desktop"    # wails.json outputfilename -> linux binary name

cd "$ROOT/desktop"

# Stamp the version resource (Windows file properties, macOS CFBundleVersion) from
# the tag. Wails feeds info.productVersion into goversioninfo and NSIS's
# VIFileVersion, both of which demand a strictly numeric X.X.X, so strip the
# leading "v" AND any prerelease suffix (a `-rc1` tag would otherwise abort the
# installer build). The full tag still rides in ldflags for the in-app version.
numver="${VERSION#v}"; numver="${numver%%-*}"
node -e 'const fs=require("fs"),f="wails.json",j=JSON.parse(fs.readFileSync(f,"utf8"));j.info.productVersion=process.argv[1];fs.writeFileSync(f,JSON.stringify(j,null,2)+"\n")' "$numver"

# NSIS installer is Windows-only (Wails requires a single windows target for -nsis).
build_args=(-clean -platform "$PLATFORM" -ldflags "-X main.version=$VERSION")
[ "$os" = windows ] && build_args+=(-nsis)
# Link cgo against WebKitGTK 4.1: 4.0 (libwebkit2gtk-4.0.so.37) is gone on
# Ubuntu 24.04+/Fedora 40+, while 4.1 ships from Ubuntu 22.04 onward.
[ "$os" = linux ] && build_args+=(-tags webkit2_41)

echo "==> wails build ${build_args[*]}"
wails build "${build_args[@]}"

mkdir -p "$ROOT/dist"

case "$os" in
darwin)
	# Wails names the bundle after outputfilename (reasonix-desktop.app); repackage
	# it as Reasonix.app for a clean user-facing name. Ad-hoc sign the copy (still
	# not notarized — the real fix is a Developer ID cert); this cuts down the
	# Gatekeeper "is damaged / can't be opened" error on a downloaded build, though
	# users may still need to clear the quarantine attribute (see desktop/README.md).
	staging=$(mktemp -d)
	app="$staging/${APPNAME}.app"
	cp -R "build/bin/reasonix-desktop.app" "$app"
	codesign --force --deep -s - "$app"
	if [ "$arch" = universal ]; then
		# One universal .app covers Intel + Apple Silicon; publish it under both
		# manifest keys so the updater's darwin-arm64/darwin-amd64 lookup finds it
		# (avoids a scarce macos-13 Intel runner).
		ditto -c -k --keepParent "$app" "$ROOT/dist/${APPNAME}-darwin-arm64.zip"
		ditto -c -k --keepParent "$app" "$ROOT/dist/${APPNAME}-darwin-amd64.zip"
	else
		ditto -c -k --keepParent "$app" "$ROOT/dist/${APPNAME}-darwin-${arch}.zip"
	fi
	rm -rf "$staging"
	;;
windows)
	# `wails build -nsis` writes the installer under build/bin; its exact name
	# varies, so glob for it and copy to a stable, platform-keyed name.
	installer=$(ls build/bin/*installer*.exe 2>/dev/null | head -n1 || true)
	[ -n "$installer" ] || { echo "no NSIS installer found in build/bin" >&2; exit 1; }
	cp "$installer" "$ROOT/dist/${APPNAME}-windows-${arch}-installer.exe"
	;;
linux)
	tar -czf "$ROOT/dist/${APPNAME}-linux-${arch}.tar.gz" -C build/bin "$BINNAME"
	;;
*)
	echo "unsupported os: $os" >&2
	exit 1
	;;
esac

echo "==> packaged into dist/:"
ls -la "$ROOT/dist"
