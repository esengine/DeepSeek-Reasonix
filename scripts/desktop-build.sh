#!/usr/bin/env bash
# Build and package the Electron desktop app for one platform. Electron's
# Chromium shell cannot cross-compile the native targets from one host, so this
# runs on a native runner per target (see .github/workflows/release-desktop.yml)
# and is invoked once per matrix entry.
#
# Output lands in <repo>/dist/ with stable, platform-keyed names that
# desktop/cmd/sign's `manifest` subcommand maps back to update.PlatformKey:
#   macOS:   Reasonix-darwin-<arch>.zip                  (ditto archive; updater channel)
#            Reasonix-darwin-universal.dmg               (drag-to-install; human download)
#   Windows: Reasonix-windows-<arch>-installer.exe       (NSIS per-user installer; updater channel)
#            Reasonix-windows-<arch>.zip                 (portable human download)
#   Linux:   Reasonix-linux-<arch>.tar.gz                (desktop + guard + CLI + app/ tree; portable updater)
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
#   - A pnpm-installed desktop workspace (this script runs
#     `pnpm --dir desktop install --frozen-lockfile` when node_modules is absent)
set -euo pipefail

PLATFORM="${1:?usage: desktop-build.sh <os/arch> <version> [channel]}"
VERSION="${2:?usage: desktop-build.sh <os/arch> <version> [channel]}"
CHANNEL="${3:-stable}"

os="${PLATFORM%/*}"
arch="${PLATFORM#*/}"

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
APPNAME="Reasonix"            # Electron productName -> Reasonix.app / Reasonix.exe
BINNAME="reasonix-desktop"    # Go desktop service (and the active version entry the launcher starts)
CLINAME="reasonix"            # bundled CLI sidecar used for remote serve upload
WINDOWS_CLINAME="reasonix-cli" # Windows cannot store Reasonix.exe and reasonix.exe separately
GUARDNAME="reasonix-guard"
LAUNCHERNAME="reasonix-launcher"
windows_resource_tool_dir=""
windows_host_include=""

# desktop/ is a nested Go module, so the Go toolchain cannot discover the
# repository VCS revision for the service binary. Link the same source identity
# into both Desktop and its CLI sidecar.
SOURCE_REVISION="$(git -C "$ROOT" rev-parse --verify HEAD)"
if ! git -C "$ROOT" diff-index --quiet HEAD --; then
	SOURCE_REVISION="$SOURCE_REVISION+dirty"
fi
# Short commit + real UTC build clock for CLI `version --verbose/--json`.
GIT_COMMIT="$(git -C "$ROOT" rev-parse --short=12 HEAD 2>/dev/null || echo unknown)"
BUILD_TIME_UTC="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
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

# Stamp the Windows version resource from the tag. goversioninfo demands a
# strictly numeric X.X.X, so strip the leading "v" AND any prerelease suffix (a
# `-rc1` tag would otherwise abort the resource stamping). The full tag still
# rides in ldflags for the in-app version.
numver="${VERSION#v}"; numver="${numver%%-*}"

# Regenerate the desktop host contract and fail on drift: the packaged shell
# embeds desktopContract.json, so a stale frontend/src/generated would ship a
# shell/service protocol mismatch. CI's desktop-prepare job runs the same check.
echo "==> desktop host contract drift check"
go run . -emit-contract frontend/src/generated
if ! git -C "$ROOT" diff --exit-code -- desktop/frontend/src/generated >/dev/null; then
	echo "desktop contract is stale - run 'cd desktop && go run . -emit-contract frontend/src/generated' and commit" >&2
	git -C "$ROOT" diff --stat -- desktop/frontend/src/generated >&2
	exit 1
fi

# The packaging script drives the frontend (build:electron) and shell builds
# through pnpm; make sure the workspace dependencies (Electron, the packager)
# are installed first. A warm store makes this a no-op.
if [ ! -d "$ROOT/desktop/node_modules/@electron/packager" ] || [ ! -d "$ROOT/desktop/electron/node_modules/electron" ]; then
	echo "==> pnpm install desktop workspace"
	pnpm --dir "$ROOT/desktop" install --frozen-lockfile
fi

# Service ldflags carry version + channel; the shell reads the same identity
# from resources/build.json written by package.mjs. macOS Developer ID builds
# enable the in-app self-update path.
service_ldflags="-X main.version=$VERSION -X main.channel=$CHANNEL $product_docs_ldflags"
[ "$os" = "darwin" ] && [ "${HAS_APPLE_CERT:-}" = "true" ] && service_ldflags="$service_ldflags -X main.macSelfUpdate=true"

# build_service compiles the Go desktop service (reasonix-desktop). It stays
# the active version entry the thin launcher starts: without --host-rpc it
# bootstraps the Electron shell from app/ and exits; the shell then spawns it
# with --host-rpc as the service (see docs/DESKTOP_SHELL_MIGRATION.md phase E).
build_service() {
	echo "==> go build Reasonix desktop service"
	mkdir -p "$(dirname "$service_out")"
	if [ "$arch" = universal ]; then
		service_tmp=$(mktemp -d)
		GOOS=darwin GOARCH=amd64 go build -trimpath -ldflags="-s -w $service_ldflags" -o "$service_tmp/amd64" .
		GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags="-s -w $service_ldflags" -o "$service_tmp/arm64" .
		lipo -create "$service_tmp/amd64" "$service_tmp/arm64" -output "$service_out"
		rm -rf "$service_tmp"
	else
		GOOS="$os" GOARCH="$arch" go build -trimpath -ldflags="-s -w $service_ldflags" -o "$service_out" .
	fi
}

# package_shell runs @electron/packager via the packaging script and leaves the
# bundle at desktop/build/electron/<os>-<arch>/ (Reasonix.app on macOS, app/
# elsewhere). It also builds the frontend (build:electron) with the channel
# threaded through REASONIX_CHANNEL.
package_shell() {
	echo "==> package Electron shell ($PLATFORM)"
	REASONIX_COMMIT="$GIT_COMMIT" REASONIX_BUILD_TIME="$BUILD_TIME_UTC" \
		node "$ROOT/desktop/packaging/package.mjs" "$PLATFORM" "$VERSION" "$CHANNEL"
}

mkdir -p "$ROOT/dist"

case "$os" in
darwin)
	service_out="$ROOT/desktop/build/bin/$BINNAME"
	build_service
	cli_out="$ROOT/desktop/build/bin/$CLINAME"
	build_cli
	package_shell

	staging=$(mktemp -d)
	app="$staging/${APPNAME}.app"
	cp -R "build/electron/${os}-${arch}/${APPNAME}.app" "$app"
	# The bundle's main executable is Electron; the Go service lives next to it
	# in Contents/MacOS and is also copied to Contents/Resources/service/ because
	# the shell's default service lookup is process.resourcesPath/service/…
	# (launchers that do not set REASONIX_DESKTOP_SERVICE still find it there).
	bundle_executable=$(/usr/libexec/PlistBuddy -c "Print :CFBundleExecutable" "$app/Contents/Info.plist")
	[ "$bundle_executable" = "$APPNAME" ] || { echo "macOS bundle executable is $bundle_executable, want $APPNAME" >&2; exit 1; }
	mkdir -p "$app/Contents/Resources/service"
	cp "$service_out" "$app/Contents/MacOS/$BINNAME"
	cp "$service_out" "$app/Contents/Resources/service/$BINNAME"
	# Contents/MacOS already holds the Electron executable "Reasonix"; on
	# case-insensitive APFS a "reasonix" sibling would overwrite it, so the
	# CLI sidecar ships next to the service copy the shell actually launches
	# (desktopCLIBinaryPath resolves it beside the running service).
	cp "$cli_out" "$app/Contents/Resources/service/$CLINAME"
	if [ -e "$app/Contents/MacOS/$GUARDNAME" ]; then
		echo "macOS bundle must not include $GUARDNAME" >&2
		exit 1
	fi
	darwin_icon="$ROOT/desktop/build/darwin/icon.icns"
	bundle_icon=$(/usr/libexec/PlistBuddy -c "Print :CFBundleIconFile" "$app/Contents/Info.plist" 2>/dev/null || true)
	case "$bundle_icon" in
	*.icns) ;;
	*) bundle_icon="$bundle_icon.icns" ;;
	esac
	[ -s "$app/Contents/Resources/$bundle_icon" ] || { echo "macOS bundle icon is missing: $bundle_icon" >&2; exit 1; }

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
		node "$ROOT/desktop/packaging/sign-macos.mjs" "$app" "$identity"
		# notarytool wants an archive, not a bare bundle: zip the .app, submit, wait,
		# then staple the ticket back onto the bundle so it verifies offline.
		ditto -c -k --keepParent "$app" "$staging/notarize.zip"
		notary_diagnostics="${APPLE_NOTARIZATION_LOG_DIR:-$ROOT/desktop/build/notarization}"
		node "$ROOT/scripts/notarize-desktop.mjs" "$staging/notarize.zip" "$app" app "$notary_diagnostics"
	else
		# Ad-hoc cuts the "is damaged" error somewhat but is NOT notarized; users may
		# still need `xattr -dr com.apple.quarantine` (see desktop/README.md).
		node "$ROOT/desktop/packaging/sign-macos.mjs" "$app" -
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
			node "$ROOT/scripts/notarize-desktop.mjs" "$dmg" "$dmg" dmg "$notary_diagnostics"
		fi
		rm -rf "$dmgsrc"
	fi
	rm -rf "$staging"
	;;
windows)
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

	installer_dir="$ROOT/desktop/build/windows/installer"
	guard_out="$installer_dir/$GUARDNAME.exe"
	build_guard
	stamp_windows_executable "$guard_out" "Reasonix Legacy Migrator" "$GUARDNAME" "$GUARDNAME.exe"
	launcher_out="$installer_dir/$LAUNCHERNAME.exe"
	echo "==> go build Windows GUI thin launcher"
	(cd "$ROOT" && GOOS=windows GOARCH="$arch" CGO_ENABLED=0 go build -trimpath \
		-ldflags="-s -w -H windowsgui -X main.version=$VERSION" -o "$launcher_out" ./cmd/reasonix-launcher)
	stamp_windows_executable "$launcher_out" "Reasonix Launcher" "$LAUNCHERNAME" "$LAUNCHERNAME.exe"
	UPDATE_HELPER="reasonix-update-helper.exe"
	echo "==> go build Windows update helper"
	GOOS=windows GOARCH="$arch" go build -trimpath -ldflags="-s -w" \
		-o "$installer_dir/$UPDATE_HELPER" ./cmd/update-helper
	stamp_windows_executable "$installer_dir/$UPDATE_HELPER" "Reasonix Update Helper" "reasonix-update-helper" "$UPDATE_HELPER"
	cli_out="$installer_dir/$WINDOWS_CLINAME.exe"
	build_cli
	stamp_windows_executable "$cli_out" "Reasonix CLI" "$WINDOWS_CLINAME" "$WINDOWS_CLINAME.exe"

	service_out="$ROOT/desktop/build/bin/$BINNAME.exe"
	build_service
	stamp_windows_executable "$service_out" "Reasonix Desktop" "$BINNAME" "$BINNAME.exe"
	# NSIS File sources live next to project.nsi; the service joins the flat
	# payload files there (package-windows-desktop.sh overwrites them with the
	# signed copies before the second pass).
	cp "$service_out" "$installer_dir/$BINNAME.exe"

	package_shell
	# The Electron bundle becomes versions/v<ver>/app/ at install time; NSIS
	# consumes it as the "app" directory next to project.nsi.
	rm -rf "$installer_dir/app"
	cp -R "build/electron/${os}-${arch}/app" "$installer_dir/app"

	# First NSIS pass: regenerate this release's uninstaller. A stale preserved
	# uninstaller must never enter the signing payload.
	rm -f "$installer_dir/reasonix-uninstall.exe"
	find "$ROOT/desktop/build/bin" -maxdepth 1 -type f -name '*installer*.exe' -delete
	arch_binary_define="ARG_REASONIX_AMD64_BINARY"
	[ "$arch" = arm64 ] && arch_binary_define="ARG_REASONIX_ARM64_BINARY"
	(
		cd "$installer_dir"
		makensis "-D${arch_binary_define}=$installer_dir/$BINNAME.exe" project.nsi
	)
	[ -s "$installer_dir/reasonix-uninstall.exe" ] || { echo "first NSIS pass did not produce reasonix-uninstall.exe" >&2; exit 1; }

	# Keep one canonical payload for SignPath: the flat Go executables plus the
	# Electron app/ tree. The release workflow signs these files, then calls
	# package-windows-desktop.sh again so both the portable archive and the
	# files embedded by NSIS carry Authenticode.
	payload_dir="$ROOT/desktop/build/windows/signing-payload"
	rm -rf -- "$payload_dir"
	mkdir -p "$payload_dir"
	for name in "$BINNAME.exe" "$GUARDNAME.exe" "$LAUNCHERNAME.exe" "$UPDATE_HELPER" "$WINDOWS_CLINAME.exe" "reasonix-uninstall.exe"; do
		cp "$installer_dir/$name" "$payload_dir/$name"
	done
	cp -R "$installer_dir/app" "$payload_dir/app"
	# signing-files.txt enumerates every PE file (flat payload + app tree); the
	# SignPath artifact configuration and the Authenticode verifier consume it.
	node "$ROOT/desktop/packaging/signing-files.mjs" "$payload_dir"
	VERSION="$VERSION" "$ROOT/scripts/package-windows-desktop.sh" "$arch" "$payload_dir"
	;;
linux)
	service_out="$ROOT/desktop/build/bin/$BINNAME"
	build_service
	# Linux still ships a one-shot migrator named reasonix-guard in the portable
	# tarball so 1.18–1.19.1 updaters can hand off.
	guard_out="$ROOT/desktop/build/bin/$GUARDNAME"
	build_guard
	launcher_out="$ROOT/desktop/build/bin/$LAUNCHERNAME"
	echo "==> go build Linux thin launcher"
	(cd "$ROOT" && GOOS=linux GOARCH="$arch" CGO_ENABLED=0 go build -trimpath \
		-ldflags="-s -w -X main.version=$VERSION" -o "$launcher_out" ./cmd/reasonix-launcher)
	cli_out="$ROOT/desktop/build/bin/$CLINAME"
	build_cli
	package_shell
	# Stage the Electron tree next to the Go binaries so the tarball and the
	# nfpm config share one source root (build/bin).
	rm -rf "build/bin/app"
	cp -R "build/electron/${os}-${arch}/app" "build/bin/app"

	for desktop_contract in \
		'Exec=reasonix-launcher' \
		'Icon=reasonix-desktop' \
		'StartupWMClass=Reasonix'; do
		grep -F -x -q "$desktop_contract" build/linux/reasonix.desktop || { echo "Linux desktop entry missing: $desktop_contract" >&2; exit 1; }
	done
	# Portable Linux tarball: service + thin launcher + one-shot migrator
	# (compat name reasonix-guard) + CLI + the Electron app/ tree. After the
	# migrator runs, Guard self-deletes.
	tar -czf "$ROOT/dist/${APPNAME}-linux-${arch}.tar.gz" -C build/bin \
		"$BINNAME" "$LAUNCHERNAME" "$GUARDNAME" "$CLINAME" app
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
	# Contract smoke: helper, policy, package identity, Electron tree, sandbox.
	deb_path="$ROOT/dist/${APPNAME}-linux-${arch}.deb"
	dpkg-deb --field "$deb_path" Package | grep -x 'reasonix-desktop' >/dev/null
	dpkg-deb --field "$deb_path" Version | grep -x "$deb_version" >/dev/null
	dpkg-deb --field "$deb_path" Depends | grep -F 'pkexec' >/dev/null
	dpkg-deb --contents "$deb_path" | grep -E 'usr/lib/reasonix/reasonix-update-helper' >/dev/null
	dpkg-deb --contents "$deb_path" | grep -E 'usr/share/polkit-1/actions/io.reasonix.desktop.update.policy' >/dev/null
	dpkg-deb --contents "$deb_path" | grep -E "usr/lib/reasonix/app/${APPNAME}" >/dev/null
	dpkg-deb --contents "$deb_path" | grep -E 'usr/lib/reasonix/app/chrome-sandbox' >/dev/null
	;;
*)
	echo "unsupported os: $os" >&2
	exit 1
	;;
esac

echo "==> packaged into dist/:"
ls -la "$ROOT/dist"
