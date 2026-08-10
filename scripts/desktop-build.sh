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
#   Linux:   Reasonix-linux-<arch>.tar.gz                (desktop + guard + CLI; portable updater)
#            Reasonix-linux-<arch>.deb                   (Debian/Ubuntu package; native updater)
#
# Usage: scripts/desktop-build.sh <os/arch> <version> [channel]
#   e.g. scripts/desktop-build.sh darwin/arm64 v1.1.0
#        scripts/desktop-build.sh darwin/arm64 v1.5.0-preview.42 preview
#
# Requirements:
#   - Go toolchain matching desktop/go.mod (>= 1.25; the `toolchain` directive
#     auto-downloads when GOTOOLCHAIN=auto)
#   - Node >= 24 and pnpm 10 (the same major versions used by CI and releases)
#   - Wails CLI matching the shared .wails-version pin (`make wails-install`)
set -euo pipefail

PLATFORM="${1:?usage: desktop-build.sh <os/arch> <version> [channel]}"
VERSION="${2:?usage: desktop-build.sh <os/arch> <version> [channel]}"
CHANNEL="${3:-stable}"

os="${PLATFORM%/*}"
arch="${PLATFORM#*/}"

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
APPNAME="Reasonix"            # wails.json productName -> Reasonix.app
BINNAME="reasonix-desktop"    # wails.json outputfilename -> linux binary name
CLINAME="reasonix"            # bundled CLI sidecar used for remote serve upload
WINDOWS_CLINAME="reasonix-cli" # Windows cannot store Reasonix.exe and reasonix.exe separately
GUARDNAME="reasonix-guard"
LAUNCHERNAME="reasonix-launcher"
windows_resource_tool_dir=""
windows_host_include=""

# desktop/ is a nested Go module, so the Go toolchain cannot discover the
# repository VCS revision for the Wails binary. Link the same source identity
# into both Desktop and its CLI sidecar before this script mutates packaging
# metadata such as wails.json.
SOURCE_REVISION="$(git -C "$ROOT" rev-parse --verify HEAD)"
if ! git -C "$ROOT" diff-index --quiet HEAD --; then
	SOURCE_REVISION="$SOURCE_REVISION+dirty"
fi
# Short commit + real UTC build clock for CLI `version --verbose/--json`.
GIT_COMMIT="$(git -C "$ROOT" rev-parse --short=12 HEAD 2>/dev/null || echo unknown)"
BUILD_TIME_UTC="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
# The remote protocol source-revision ldflag was removed with Remote Workbench.
product_docs_ldflags="-X reasonix/internal/productdocs.linkedVersion=$VERSION -X reasonix/internal/productdocs.linkedRevision=$SOURCE_REVISION"
cli_identity_ldflags="-X main.version=$VERSION -X main.gitCommit=$GIT_COMMIT -X main.buildTimeUTC=$BUILD_TIME_UTC $product_docs_ldflags"

cleanup() {
	if [ -n "$windows_resource_tool_dir" ]; then
		rm -rf "$windows_resource_tool_dir"
	fi
	if [ -n "$windows_host_include" ]; then
		rm -f "$windows_host_include"
	fi
}
trap cleanup EXIT

cd "$ROOT/desktop"

# build_guard produces the one-shot legacy migrator still named reasonix-guard
# in compatibility payloads for 1.18–1.19.1 updaters. Source is intentionally
# separate from the removed Guard recovery product.
build_guard() {
	echo "==> go build Reasonix legacy migrator (compat name reasonix-guard)"
	mkdir -p "$(dirname "$guard_out")"
	if [ "$arch" = universal ]; then
		guard_tmp=$(mktemp -d)
		(cd "$ROOT" && GOOS=darwin GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.version=$VERSION" -o "$guard_tmp/amd64" ./cmd/reasonix-legacy-migrator)
		(cd "$ROOT" && GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.version=$VERSION" -o "$guard_tmp/arm64" ./cmd/reasonix-legacy-migrator)
		lipo -create "$guard_tmp/amd64" "$guard_tmp/arm64" -output "$guard_out"
		rm -rf "$guard_tmp"
	else
		(cd "$ROOT" && GOOS="$os" GOARCH="$arch" CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.version=$VERSION" -o "$guard_out" ./cmd/reasonix-legacy-migrator)
	fi
}

build_cli() {
	echo "==> go build Reasonix CLI sidecar"
	mkdir -p "$(dirname "$cli_out")"
	if [ "$arch" = universal ]; then
		cli_tmp=$(mktemp -d)
		(cd "$ROOT" && GOOS=darwin GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w $cli_identity_ldflags" -o "$cli_tmp/amd64" ./cmd/reasonix)
		(cd "$ROOT" && GOOS=darwin GOARCH=arm64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w $cli_identity_ldflags" -o "$cli_tmp/arm64" ./cmd/reasonix)
		lipo -create "$cli_tmp/amd64" "$cli_tmp/arm64" -output "$cli_out"
		rm -rf "$cli_tmp"
	else
		(cd "$ROOT" && GOOS="$os" GOARCH="$arch" CGO_ENABLED=0 go build -trimpath -ldflags="-s -w $cli_identity_ldflags" -o "$cli_out" ./cmd/reasonix)
	fi
}

stamp_windows_executable() {
	local target="$1"
	local description="$2"
	local internal_name="$3"
	local original_filename="$4"
	"$windows_resource_tool" \
		-exe "$target" \
		-icon "$ROOT/desktop/build/windows/icon.ico" \
		-version "$numver" \
		-description "$description" \
		-internal-name "$internal_name" \
		-original-filename "$original_filename"
}

# Stamp the version resource (Windows file properties, macOS CFBundleVersion) from
# the tag. Wails feeds info.productVersion into goversioninfo and NSIS's
# VIFileVersion, both of which demand a strictly numeric X.X.X, so strip the
# leading "v" AND any prerelease suffix (a `-rc1` tag would otherwise abort the
# installer build). The full tag still rides in ldflags for the in-app version.
numver="${VERSION#v}"; numver="${numver%%-*}"
node -e 'const fs=require("fs"),f="wails.json",j=JSON.parse(fs.readFileSync(f,"utf8"));j.info.productVersion=process.argv[1];fs.writeFileSync(f,JSON.stringify(j,null,2)+"\n")' "$numver"

# NSIS installer is Windows-only (Wails requires a single windows target for -nsis).
ldflags="-X main.version=$VERSION -X main.channel=$CHANNEL $product_docs_ldflags"
[ "$os" = "darwin" ] && [ "${HAS_APPLE_CERT:-}" = "true" ] && ldflags="$ldflags -X main.macSelfUpdate=true"
UPDATE_HELPER="reasonix-update-helper.exe"
if [ "$os" = windows ]; then
	windows_resource_tool_dir=$(mktemp -d)
	windows_host_include="$ROOT/desktop/build/windows/installer/reasonix_host.nsh"
	case "$(uname -s 2>/dev/null || printf '%s' unknown)" in
		Darwin* | Linux* | FreeBSD*)
			printf '%s\n' '!define REASONIX_UNINST_FINALIZE '\''/bin/cp -f "%1" "reasonix-uninstall.exe"'\''' >"$windows_host_include"
			;;
		*)
			printf '%s\n' '!define REASONIX_UNINST_FINALIZE '\''cmd.exe /C copy /Y "%1" "reasonix-uninstall.exe" >NUL'\''' >"$windows_host_include"
			;;
	esac
	windows_resource_tool="$windows_resource_tool_dir/reasonix-windows-resource.exe"
	echo "==> build Windows resource stamper"
	go build -trimpath -o "$windows_resource_tool" ./cmd/windows-resource
	guard_out="$ROOT/desktop/build/windows/installer/$GUARDNAME.exe"
	build_guard
	stamp_windows_executable "$guard_out" "Reasonix Legacy Migrator" "$GUARDNAME" "$GUARDNAME.exe"
	launcher_out="$ROOT/desktop/build/windows/installer/$LAUNCHERNAME.exe"
	echo "==> go build Windows GUI thin launcher"
	(cd "$ROOT" && GOOS=windows GOARCH="$arch" CGO_ENABLED=0 go build -trimpath \
		-ldflags="-s -w -H windowsgui -X main.version=$VERSION" -o "$launcher_out" ./cmd/reasonix-launcher)
	stamp_windows_executable "$launcher_out" "Reasonix Launcher" "$LAUNCHERNAME" "$LAUNCHERNAME.exe"
	echo "==> go build Windows update helper"
	GOOS=windows GOARCH="$arch" go build -trimpath -ldflags="-s -w" \
		-o "build/windows/installer/$UPDATE_HELPER" ./cmd/update-helper
	stamp_windows_executable "build/windows/installer/$UPDATE_HELPER" "Reasonix Update Helper" "reasonix-update-helper" "$UPDATE_HELPER"
	cli_out="$ROOT/desktop/build/windows/installer/$WINDOWS_CLINAME.exe"
	build_cli
	stamp_windows_executable "$cli_out" "Reasonix CLI" "$WINDOWS_CLINAME" "$WINDOWS_CLINAME.exe"
	# The first NSIS pass must regenerate this release's uninstaller; a stale
	# preserved file must never enter the signing payload.
	rm -f "build/windows/installer/reasonix-uninstall.exe"
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
if [ "$os" != windows ]; then
	# Linux still ships a one-shot migrator named reasonix-guard in the portable
	# tarball so 1.18–1.19.1 updaters can hand off. macOS does not bundle Guard.
	if [ "$os" = linux ]; then
		guard_out="$ROOT/desktop/build/bin/$GUARDNAME"
		build_guard
		launcher_out="$ROOT/desktop/build/bin/$LAUNCHERNAME"
		echo "==> go build Linux thin launcher"
		(cd "$ROOT" && GOOS=linux GOARCH="$arch" CGO_ENABLED=0 go build -trimpath \
			-ldflags="-s -w -X main.version=$VERSION" -o "$launcher_out" ./cmd/reasonix-launcher)
	fi
	cli_out="$ROOT/desktop/build/bin/$CLINAME"
	build_cli
fi

mkdir -p "$ROOT/dist"

case "$os" in
darwin)
	# Wails names the bundle after outputfilename (reasonix-desktop.app); repackage
	# it as Reasonix.app for a clean user-facing name.
	staging=$(mktemp -d)
	app="$staging/${APPNAME}.app"
	cp -R "build/bin/reasonix-desktop.app" "$app"
	# v1.20+: no Guard in the App bundle. CLI remains a sibling helper for
	# terminal workflows; LaunchServices must own the Wails process directly.
	cp "$cli_out" "$app/Contents/MacOS/$CLINAME"
	bundle_executable=$(/usr/libexec/PlistBuddy -c "Print :CFBundleExecutable" "$app/Contents/Info.plist")
	[ "$bundle_executable" = "$BINNAME" ] || { echo "macOS bundle executable is $bundle_executable, want $BINNAME" >&2; exit 1; }
	if [ -e "$app/Contents/MacOS/$GUARDNAME" ]; then
		echo "macOS bundle must not include $GUARDNAME" >&2
		exit 1
	fi
	bundle_icon=$(/usr/libexec/PlistBuddy -c "Print :CFBundleIconFile" "$app/Contents/Info.plist")
	case "$bundle_icon" in
	*.icns) ;;
	*) bundle_icon="$bundle_icon.icns" ;;
	esac
	darwin_icon="$ROOT/desktop/build/darwin/icon.icns"
	[ -s "$darwin_icon" ] || { echo "macOS source icon is missing: $darwin_icon" >&2; exit 1; }
	# Wails v2 always regenerates iconfile.icns from build/appicon.png. Replace it
	# with the platform-specific asset before signing so the macOS safe area does
	# not force the shared Windows/Linux artwork to shrink as well.
	cp "$darwin_icon" "$app/Contents/Resources/$bundle_icon"
	[ -s "$app/Contents/Resources/$bundle_icon" ] || { echo "macOS bundle icon is missing: $bundle_icon" >&2; exit 1; }
	cmp -s "$darwin_icon" "$app/Contents/Resources/$bundle_icon" || { echo "macOS bundle icon replacement failed: $bundle_icon" >&2; exit 1; }

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
		fi
		rm -rf "$dmgsrc"
	fi
	rm -rf "$staging"
	;;
windows)
	# Keep one canonical flat payload for SignPath. The release workflow signs
	# these files, then calls package-windows-desktop.sh again so both the
	# portable archive and the files embedded by NSIS carry Authenticode.
	payload_dir="$ROOT/desktop/build/windows/signing-payload"
	rm -rf -- "$payload_dir"
	mkdir -p "$payload_dir"
	cp "build/bin/$BINNAME.exe" "$payload_dir/$BINNAME.exe"
	cp "build/windows/installer/$UPDATE_HELPER" "$payload_dir/$UPDATE_HELPER"
	cp "$launcher_out" "$payload_dir/$LAUNCHERNAME.exe"
	cp "$guard_out" "$payload_dir/$GUARDNAME.exe"
	cp "build/windows/installer/$WINDOWS_CLINAME.exe" "$payload_dir/$WINDOWS_CLINAME.exe"
	cp "build/windows/installer/reasonix-uninstall.exe" "$payload_dir/reasonix-uninstall.exe"
	"$ROOT/scripts/package-windows-desktop.sh" "$arch" "$payload_dir"
	;;
linux)
	for desktop_contract in \
		'Exec=reasonix-launcher' \
		'Icon=reasonix-desktop' \
		'StartupWMClass=reasonix-desktop'; do
		grep -F -x -q "$desktop_contract" build/linux/reasonix.desktop || { echo "Linux desktop entry missing: $desktop_contract" >&2; exit 1; }
	done
	# Portable Linux tarball: desktop + thin launcher + one-shot migrator
	# (compat name reasonix-guard) + CLI. After migrator runs, Guard self-deletes.
	tar -czf "$ROOT/dist/${APPNAME}-linux-${arch}.tar.gz" -C build/bin \
		"$BINNAME" "$LAUNCHERNAME" "$GUARDNAME" "$CLINAME"
	# Build the privileged update helper shipped inside the .deb. Portable tarball
	# installs do not need it; only the dpkg package installs helper + Polkit policy.
	echo "==> go build reasonix-update-helper"
	GOOS=linux GOARCH="$arch" CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.version=$VERSION" \
		-o "build/bin/reasonix-update-helper" ./cmd/update-helper
	# .deb for Debian/Ubuntu. Portable updater still uses the tarball under
	# platforms[]; .deb is published under native_packages. Debian versions use
	# "~" for prereleases so 1.18.0~rc.1 < 1.18.0 (policy version ordering).
	# Extra "-" inside the prerelease label becomes "." (Debian policy).
	ver_body="${VERSION#v}"
	if [[ "$ver_body" == *-* ]]; then
		deb_base="${ver_body%%-*}"
		deb_pre="${ver_body#*-}"
		deb_pre="${deb_pre//-/.}"
		deb_version="${deb_base}~${deb_pre}"
	else
		deb_version="$ver_body"
	fi
	DEB_VERSION="$deb_version" DEB_ARCH="$arch" \
		nfpm package --config build/linux/nfpm.yaml --packager deb \
		--target "$ROOT/dist/${APPNAME}-linux-${arch}.deb"
	# Contract smoke: helper, policy, package identity, and pkexec dependency.
	deb_path="$ROOT/dist/${APPNAME}-linux-${arch}.deb"
	dpkg-deb --field "$deb_path" Package | grep -x 'reasonix-desktop' >/dev/null
	dpkg-deb --field "$deb_path" Version | grep -x "$deb_version" >/dev/null
	dpkg-deb --field "$deb_path" Depends | grep -F 'pkexec' >/dev/null
	dpkg-deb --contents "$deb_path" | grep -E 'usr/lib/reasonix/reasonix-update-helper' >/dev/null
	dpkg-deb --contents "$deb_path" | grep -E 'usr/share/polkit-1/actions/io.reasonix.desktop.update.policy' >/dev/null
	;;
*)
	echo "unsupported os: $os" >&2
	exit 1
	;;
esac

echo "==> packaged into dist/:"
ls -la "$ROOT/dist"
