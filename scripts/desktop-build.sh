#!/usr/bin/env bash
# Build and package the Wails desktop app for one platform. Wails cannot
# cross-compile a CGO+webview binary, so this runs on a native runner per target
# (see .github/workflows/release-desktop.yml) and is invoked once per matrix entry.
#
# Output lands in <repo>/dist/ with stable, platform-keyed names that
# desktop/cmd/sign's `manifest` subcommand maps back to update.PlatformKey:
#   macOS:   Reasonix-darwin-<arch>.zip                  (ditto archive; updater channel)
#            Reasonix-darwin-universal.dmg               (drag-to-install; human download)
#   Windows: Reasonix-windows-<arch>-installer.exe       (NSIS per-user installer; updater channel)
#            Reasonix-windows-<arch>.zip                 (portable human download)
#   Linux:   Reasonix-linux-<arch>.tar.gz                (bare binary; updater channel)
#            Reasonix-linux-<arch>.deb                   (Debian/Ubuntu package; human download)
#
# Usage: scripts/desktop-build.sh <os/arch> <version> [channel]
#   e.g. scripts/desktop-build.sh darwin/arm64 v1.1.0
#        scripts/desktop-build.sh darwin/arm64 v1.5.0-canary.20260608.42 canary
set -euo pipefail

PLATFORM="${1:?usage: desktop-build.sh <os/arch> <version> [channel]}"
VERSION="${2:?usage: desktop-build.sh <os/arch> <version> [channel]}"
CHANNEL="${3:-stable}"

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
build_args=(-clean -platform "$PLATFORM" -ldflags "-X main.version=$VERSION -X main.channel=$CHANNEL")
[ "$os" = windows ] && build_args+=(-nsis -webview2 embed)
# Link cgo against WebKitGTK 4.1: 4.0 (libwebkit2gtk-4.0.so.37) is gone on
# Ubuntu 24.04+/Fedora 40+, while 4.1 ships from Ubuntu 22.04 onward.
[ "$os" = linux ] && build_args+=(-tags webkit2_41)

echo "==> wails build ${build_args[*]}"
wails build "${build_args[@]}"

mkdir -p "$ROOT/dist"

case "$os" in
darwin)
	# Wails names the bundle after outputfilename (reasonix-desktop.app); repackage
	# it as Reasonix.app for a clean user-facing name.
	staging=$(mktemp -d)
	app="$staging/${APPNAME}.app"
	cp -R "build/bin/reasonix-desktop.app" "$app"

	# Two signing paths, selected by HAS_APPLE_CERT (set by release-desktop.yml when
	# the APPLE_* secrets are present). With a real Developer ID cert + notarization
	# key we sign with a hardened runtime, notarize, and staple — a downloaded build
	# then opens with no Gatekeeper prompt. Without it we ad-hoc sign as before (still
	# un-notarized; users clear the quarantine attribute per desktop/README.md). The
	# fallback keeps fork/local builds working with no secrets configured.
	if [ "${HAS_APPLE_CERT:-}" = "true" ]; then
		identity="$(security find-identity -v -p codesigning | awk -F'"' '/Developer ID Application/{print $2; exit}')"
		[ -n "$identity" ] || { echo "HAS_APPLE_CERT=true but no 'Developer ID Application' identity found in the keychain" >&2; exit 1; }
		echo "==> codesign (Developer ID): $identity"
		codesign --force --deep --timestamp --options runtime \
			--entitlements "$ROOT/desktop/build/darwin/entitlements.plist" \
			-s "$identity" "$app"
		# notarytool wants an archive, not a bare bundle: zip the .app, submit, wait,
		# then staple the ticket back onto the bundle so it verifies offline.
		ditto -c -k --keepParent "$app" "$staging/notarize.zip"
		echo "==> notarytool submit (app)"
		xcrun notarytool submit "$staging/notarize.zip" \
			--key "$APPLE_API_KEY_PATH" --key-id "$APPLE_API_KEY_ID" \
			--issuer "$APPLE_API_ISSUER_ID" --wait
		xcrun stapler staple "$app"
	else
		# Ad-hoc cuts the "is damaged" error somewhat but is NOT notarized; users may
		# still need `xattr -dr com.apple.quarantine` (see desktop/README.md).
		codesign --force --deep -s - "$app"
	fi

	if [ "$arch" = universal ]; then
		# One universal .app covers Intel + Apple Silicon; publish it under both
		# manifest keys so the updater's darwin-arm64/darwin-amd64 lookup finds it
		# (avoids a scarce macos-13 Intel runner).
		ditto -c -k --keepParent "$app" "$ROOT/dist/${APPNAME}-darwin-arm64.zip"
		ditto -c -k --keepParent "$app" "$ROOT/dist/${APPNAME}-darwin-amd64.zip"
	else
		ditto -c -k --keepParent "$app" "$ROOT/dist/${APPNAME}-darwin-${arch}.zip"
	fi
	# A drag-to-Applications .dmg for first-time human download. Named -universal so
	# cmd/sign's substring match (darwin-arm64/darwin-amd64) skips it: the .zip stays
	# the updater channel, the .dmg is release-page only. create-dmg can exit nonzero
	# while still writing the image, so gate on the file existing, not the exit code.
	dmgsrc=$(mktemp -d)
	cp -R "$app" "$dmgsrc/${APPNAME}.app"
	dmg="$ROOT/dist/${APPNAME}-darwin-universal.dmg"
	create-dmg \
		--volname "$APPNAME" \
		--window-size 540 380 \
		--icon-size 110 \
		--icon "${APPNAME}.app" 150 190 \
		--app-drop-link 390 190 \
		--no-internet-enable \
		"$dmg" "$dmgsrc" || true
	[ -f "$dmg" ] || { echo "create-dmg did not produce $dmg" >&2; exit 1; }
	# The .dmg is a separately-downloaded artifact, so sign + notarize + staple the
	# disk image itself too — the stapled .app inside isn't enough for the image.
	if [ "${HAS_APPLE_CERT:-}" = "true" ]; then
		codesign --force --timestamp -s "$identity" "$dmg"
		echo "==> notarytool submit (dmg)"
		xcrun notarytool submit "$dmg" \
			--key "$APPLE_API_KEY_PATH" --key-id "$APPLE_API_KEY_ID" \
			--issuer "$APPLE_API_ISSUER_ID" --wait
		xcrun stapler staple "$dmg"
	fi
	rm -rf "$staging" "$dmgsrc"
	;;
windows)
	# `wails build -nsis` writes the installer under build/bin; its exact name
	# varies, so glob for it and copy to a stable, platform-keyed name.
	installer=$(ls build/bin/*installer*.exe 2>/dev/null | head -n1 || true)
	[ -n "$installer" ] || { echo "no NSIS installer found in build/bin" >&2; exit 1; }
	cp "$installer" "$ROOT/dist/${APPNAME}-windows-${arch}-installer.exe"
	portable=$(find build/bin -maxdepth 1 -type f -name "*.exe" ! -name "*installer*.exe" | head -n1 || true)
	[ -n "$portable" ] || { echo "no portable Windows exe found in build/bin" >&2; exit 1; }
	staging=$(mktemp -d)
	cp "$portable" "$staging/${APPNAME}.exe"
	src_win=$(cygpath -w "$staging/${APPNAME}.exe")
	zip_win=$(cygpath -w "$ROOT/dist/${APPNAME}-windows-${arch}.zip")
	powershell.exe -NoProfile -Command "Compress-Archive -Force -LiteralPath '$src_win' -DestinationPath '$zip_win'"
	rm -rf "$staging"
	;;
linux)
	tar -czf "$ROOT/dist/${APPNAME}-linux-${arch}.tar.gz" -C build/bin "$BINNAME"
	# Also build a .deb for Debian/Ubuntu users (goreleaser/nfpm; see
	# desktop/build/linux/nfpm.yaml). Human-download only: the Linux updater channel
	# stays the tarball and cmd/sign's manifest skips .deb files. nfpm reads
	# $DEB_VERSION/$DEB_ARCH — dpkg wants a strict numeric version, so reuse numver.
	DEB_VERSION="$numver" DEB_ARCH="$arch" \
		nfpm package --config build/linux/nfpm.yaml --packager deb \
		--target "$ROOT/dist/${APPNAME}-linux-${arch}.deb"
	;;
*)
	echo "unsupported os: $os" >&2
	exit 1
	;;
esac

echo "==> packaged into dist/:"
ls -la "$ROOT/dist"
