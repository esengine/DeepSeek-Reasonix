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
#   Linux:   Reasonix-linux-<arch>.tar.gz                (binary + Chromium; updater channel)
#            Reasonix-linux-<arch>.deb                   (Debian/Ubuntu package; human download)
#
# Usage: scripts/desktop-build.sh <os/arch> <version> [channel]
#   e.g. scripts/desktop-build.sh darwin/universal v1.1.0
#        scripts/desktop-build.sh darwin/universal v1.5.0-canary.20260608.42 canary
set -euo pipefail

PLATFORM="${1:?usage: desktop-build.sh <os/arch> <version> [channel]}"
VERSION="${2:?usage: desktop-build.sh <os/arch> <version> [channel]}"
CHANNEL="${3:-stable}"

os="${PLATFORM%/*}"
arch="${PLATFORM#*/}"

if [ "$os" = darwin ] && [ "$arch" != universal ]; then
	echo "macOS release packaging requires darwin/universal so both updater architectures and the universal DMG are produced" >&2
	exit 1
fi

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
APPNAME="Reasonix"            # wails.json productName -> Reasonix.app
BINNAME="reasonix-desktop"    # wails.json outputfilename -> linux binary name

cd "$ROOT/desktop"

prepare_chromium() {
	local target="$1"
	echo "==> prepare bundled Chromium ($target)"
	go run ./cmd/chromium-runtime prepare --platform "$target"
}

verify_chromium() {
	local target="$1"
	go run ./cmd/chromium-runtime verify --platform "$target"
}

# The universal Wails binary needs one native Chromium runtime per macOS slice.
# Every other build has a single platform runtime. Downloads happen here only;
# the application itself never contains a downloader.
if [ "$PLATFORM" = "darwin/universal" ]; then
	prepare_chromium darwin/amd64
	prepare_chromium darwin/arm64
else
	prepare_chromium "$PLATFORM"
fi

# Stamp the version resource (Windows file properties, macOS CFBundleVersion) from
# the tag. Wails feeds info.productVersion into goversioninfo and NSIS's
# VIFileVersion, both of which demand a strictly numeric X.X.X, so strip the
# leading "v" AND any prerelease suffix (a `-rc1` tag would otherwise abort the
# installer build). The full tag still rides in ldflags for the in-app version.
numver="${VERSION#v}"; numver="${numver%%-*}"
node -e 'const fs=require("fs"),f="wails.json",j=JSON.parse(fs.readFileSync(f,"utf8"));j.info.productVersion=process.argv[1];fs.writeFileSync(f,JSON.stringify(j,null,2)+"\n")' "$numver"

# NSIS installer is Windows-only (Wails requires a single windows target for -nsis).
ldflags="-X main.version=$VERSION -X main.channel=$CHANNEL"
[ "$os" = "darwin" ] && [ "${HAS_APPLE_CERT:-}" = "true" ] && ldflags="$ldflags -X main.macSelfUpdate=true"
UPDATE_HELPER="reasonix-update-helper.exe"
if [ "$os" = windows ]; then
	echo "==> go build Windows update helper"
	GOOS=windows GOARCH="$arch" go build -trimpath -ldflags="-s -w" \
		-o "build/windows/installer/$UPDATE_HELPER" ./cmd/update-helper
fi
build_args=()
[ "${DESKTOP_BUILD_CLEAN:-1}" != "0" ] && build_args+=(-clean)
build_args+=(-platform "$PLATFORM" -ldflags "$ldflags")
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
	# it as Reasonix.app for a clean user-facing name. Updater archives carry one
	# Chromium architecture; the universal DMG carries both.
	staging=$(mktemp -d)
	base_app="build/bin/reasonix-desktop.app"

	if [ "${HAS_APPLE_CERT:-}" = "true" ]; then
		identity="$(security find-identity -v -p codesigning | awk -F'"' '/Developer ID Application/{print $2; exit}')"
		[ -n "$identity" ] || { echo "HAS_APPLE_CERT=true but no 'Developer ID Application' identity found in the keychain" >&2; exit 1; }
	fi

	assemble_mac_app() {
		local variant="$1"
		local app="$2"
		mkdir -p "$(dirname "$app")"
		cp -R "$base_app" "$app"
		mkdir -p "$app/Contents/Resources/chromium"
		if [ "$variant" = universal ]; then
			for runtime_arch in amd64 arm64; do
				verify_chromium "darwin/$runtime_arch"
				mkdir -p "$app/Contents/Resources/chromium/$runtime_arch"
				cp -R "build/runtime/chromium/darwin-$runtime_arch/." "$app/Contents/Resources/chromium/$runtime_arch/"
			done
		else
			verify_chromium "darwin/$variant"
			mkdir -p "$app/Contents/Resources/chromium/$variant"
			cp -R "build/runtime/chromium/darwin-$variant/." "$app/Contents/Resources/chromium/$variant/"
		fi
	}

	sign_mac_app() {
		local variant="$1"
		local app="$2"
		local nested
		while IFS= read -r nested; do
			[ -n "$nested" ] || continue
			if [ "${HAS_APPLE_CERT:-}" = "true" ]; then
				codesign --force --deep --timestamp --options runtime \
					--preserve-metadata=entitlements,flags,runtime -s "$identity" "$nested"
			else
				codesign --force --deep --preserve-metadata=entitlements,flags,runtime -s - "$nested"
			fi
			codesign --verify --deep --strict --verbose=2 "$nested"
		done < <(find "$app/Contents/Resources/chromium" -type d -name Chromium.app -print)

		if [ "${HAS_APPLE_CERT:-}" = "true" ]; then
			echo "==> codesign $variant (Developer ID): $identity"
			codesign --force --deep --timestamp --options runtime \
				--entitlements "$ROOT/desktop/build/darwin/entitlements.plist" \
				-s "$identity" "$app"
			local notarize_zip="$staging/notarize-$variant.zip"
			ditto -c -k --keepParent "$app" "$notarize_zip"
			echo "==> notarytool submit ($variant app)"
			xcrun notarytool submit "$notarize_zip" \
				--key "$APPLE_API_KEY_PATH" --key-id "$APPLE_API_KEY_ID" \
				--issuer "$APPLE_API_ISSUER_ID" --wait
			xcrun stapler staple "$app"
			xcrun stapler validate "$app"
			spctl --assess --type execute --verbose=2 "$app"
		else
			codesign --force --deep -s - "$app"
		fi
		codesign --verify --deep --strict --verbose=2 "$app"
	}

	for runtime_arch in arm64 amd64; do
		app="$staging/$runtime_arch/${APPNAME}.app"
		assemble_mac_app "$runtime_arch" "$app"
		sign_mac_app "$runtime_arch" "$app"
		ditto -c -k --keepParent "$app" "$ROOT/dist/${APPNAME}-darwin-${runtime_arch}.zip"
	done

	app="$staging/universal/${APPNAME}.app"
	assemble_mac_app universal "$app"
	sign_mac_app universal "$app"

	if [ "${DESKTOP_BUILD_SKIP_DMG:-0}" = "1" ]; then
		echo "==> skip DMG packaging (DESKTOP_BUILD_SKIP_DMG=1)"
	else
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
			xcrun stapler validate "$dmg"
		fi
		rm -rf "$dmgsrc"
	fi
	rm -rf "$staging"
	;;
windows)
	verify_chromium "windows/$arch"
	# `wails build -nsis` writes the installer under build/bin; its exact name
	# varies, so glob for it and copy to a stable, platform-keyed name.
	installer=$(ls build/bin/*installer*.exe 2>/dev/null | head -n1 || true)
	[ -n "$installer" ] || { echo "no NSIS installer found in build/bin" >&2; exit 1; }
	cp "$installer" "$ROOT/dist/${APPNAME}-windows-${arch}-installer.exe"
	portable=$(find build/bin -maxdepth 1 -type f -name "*.exe" ! -name "*installer*.exe" | head -n1 || true)
	[ -n "$portable" ] || { echo "no portable Windows exe found in build/bin" >&2; exit 1; }
	staging=$(mktemp -d)
	cp "$portable" "$staging/${APPNAME}.exe"
	helper="build/windows/installer/$UPDATE_HELPER"
	if [ -f "$helper" ]; then
		cp "$helper" "$staging/$UPDATE_HELPER"
	fi
	cp -R "build/runtime/chromium/windows-$arch" "$staging/chromium"
	src_win=$(cygpath -w "$staging")
	zip_win=$(cygpath -w "$ROOT/dist/${APPNAME}-windows-${arch}.zip")
	powershell.exe -NoProfile -Command "Compress-Archive -Force -Path '$src_win\\*' -DestinationPath '$zip_win'"
	rm -rf "$staging"
	;;
linux)
	verify_chromium "linux/$arch"
	staging=$(mktemp -d)
	cp "build/bin/$BINNAME" "$staging/$BINNAME"
	cp -R "build/runtime/chromium/linux-$arch" "$staging/chromium"
	tar -czf "$ROOT/dist/${APPNAME}-linux-${arch}.tar.gz" -C "$staging" "$BINNAME" chromium
	rm -rf "$staging"
	# Also build a .deb for Debian/Ubuntu users (goreleaser/nfpm; see
	# desktop/build/linux/nfpm.yaml). Human-download only: the Linux updater channel
	# stays the tarball and cmd/sign's manifest skips .deb files. nfpm reads
	# $DEB_VERSION/$DEB_ARCH — dpkg wants a strict numeric version, so reuse numver.
	DEB_VERSION="$numver" DEB_ARCH="$arch" CHROMIUM_PLATFORM="linux-$arch" \
		nfpm package --config build/linux/nfpm.yaml --packager deb \
		--target "$ROOT/dist/${APPNAME}-linux-${arch}.deb"
	;;
*)
	echo "unsupported os: $os" >&2
	exit 1
	;;
esac

echo "==> verify packaged Chromium layout ($PLATFORM)"
package_check=$(mktemp -d)
case "$os" in
darwin)
	for runtime_arch in arm64 amd64; do
		archive="$ROOT/dist/${APPNAME}-darwin-${runtime_arch}.zip"
		variant="$package_check/$runtime_arch"
		mkdir -p "$variant"
		ditto -x -k "$archive" "$variant"
		go run ./cmd/chromium-runtime verify --platform "darwin/$runtime_arch" \
			--output "$variant/${APPNAME}.app/Contents/Resources/chromium/$runtime_arch"
		codesign --verify --deep --strict --verbose=2 "$variant/${APPNAME}.app"
	done
	mountpoint="$package_check/dmg"
	mkdir -p "$mountpoint"
	hdiutil attach "$ROOT/dist/${APPNAME}-darwin-universal.dmg" -readonly -nobrowse -mountpoint "$mountpoint" >/dev/null
	for runtime_arch in amd64 arm64; do
		go run ./cmd/chromium-runtime verify --platform "darwin/$runtime_arch" \
			--output "$mountpoint/${APPNAME}.app/Contents/Resources/chromium/$runtime_arch"
	done
	codesign --verify --deep --strict --verbose=2 "$mountpoint/${APPNAME}.app"
	hdiutil detach "$mountpoint" >/dev/null
	;;
windows)
	archive="$ROOT/dist/${APPNAME}-windows-${arch}.zip"
	archive_win=$(cygpath -w "$archive")
	package_check_win=$(cygpath -w "$package_check")
	powershell.exe -NoProfile -Command "Expand-Archive -Force -LiteralPath '$archive_win' -DestinationPath '$package_check_win'"
	go run ./cmd/chromium-runtime verify --platform "windows/$arch" \
		--output "$package_check/chromium"
	;;
linux)
	archive="$ROOT/dist/${APPNAME}-linux-${arch}.tar.gz"
	mkdir -p "$package_check/tar" "$package_check/deb"
	tar -xzf "$archive" -C "$package_check/tar"
	go run ./cmd/chromium-runtime verify --platform "linux/$arch" \
		--output "$package_check/tar/chromium"
	dpkg-deb -x "$ROOT/dist/${APPNAME}-linux-${arch}.deb" "$package_check/deb"
	go run ./cmd/chromium-runtime verify --platform "linux/$arch" \
		--output "$package_check/deb/usr/lib/reasonix/chromium"
	;;
esac
rm -rf "$package_check"

echo "==> packaged into dist/:"
ls -la "$ROOT/dist"