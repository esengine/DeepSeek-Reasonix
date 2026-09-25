#!/usr/bin/env bash
# Verify that a stable orchestration produced every public release channel this
# line owns: the CLI and Desktop GitHub releases and the Desktop site surfaces.
set -euo pipefail

repository="${RELEASE_REPOSITORY:?RELEASE_REPOSITORY is required}"
version="${RELEASE_VERSION:?RELEASE_VERSION is required}"
cli_tag="${CLI_TAG:?CLI_TAG is required}"
desktop_tag="${DESKTOP_TAG:?DESKTOP_TAG is required}"
script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
verify_homepage="${VERIFY_HOMEPAGE:-false}"
site_only="${VERIFY_PUBLIC_SITE_ONLY:-false}"
operation="${RELEASE_OPERATION:-publish}"
ledger_output="${RELEASE_LEDGER_OUTPUT:-}"

if [[ ! "$version" =~ ^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]]; then
	echo "::error::RELEASE_VERSION must be stable semver, got: $version" >&2
	exit 1
fi
if [ "$cli_tag" != "v$version" ] || [ "$desktop_tag" != "desktop-v$version" ]; then
	echo "::error::release tags do not match version $version: cli=$cli_tag desktop=$desktop_tag" >&2
	exit 1
fi
case "$operation" in publish | recover) ;; *) echo "::error::RELEASE_OPERATION must be publish or recover" >&2; exit 1 ;; esac

release_git_url="https://github.com/${repository}.git"
cli_sha="$(git ls-remote --tags --refs "$release_git_url" "refs/tags/$cli_tag" | awk 'NR == 1 {print $1}')"
npm_sha="$(git ls-remote --tags --refs "$release_git_url" "refs/tags/npm-v$version" | awk 'NR == 1 {print $1}')"
desktop_sha="$(git ls-remote --tags --refs "$release_git_url" "refs/tags/$desktop_tag" | awk 'NR == 1 {print $1}')"
if [ -z "$cli_sha" ] || [ "$cli_sha" != "$npm_sha" ] || [ "$cli_sha" != "$desktop_sha" ]; then
	echo "::error::release tags are missing or do not identify one immutable commit" >&2
	exit 1
fi
if [ -n "${RELEASE_EXPECTED_SHA:-}" ] && [ "$cli_sha" != "$RELEASE_EXPECTED_SHA" ]; then
	echo "::error::public release identity differs from the verified source SHA" >&2
	exit 1
fi

tmp_dir="$(mktemp -d "${TMPDIR:-/tmp}/reasonix-release-postflight.XXXXXX")"
cleanup() {
	case "$tmp_dir" in
	*/reasonix-release-postflight.*) rm -rf -- "$tmp_dir" ;;
	*) echo "refusing to clean unexpected postflight directory: $tmp_dir" >&2 ;;
	esac
}
trap cleanup EXIT

verify_site() {
	local manifest="$tmp_dir/desktop-pointer.json"
	local homepage="$tmp_dir/homepage.html"
	local changelog="$tmp_dir/changelog.html"
	bash "$script_dir/fetch-stable-release-manifest.sh" "$version" "$manifest"
	jq -e --arg version "v$version" '
		.version == $version and
		([.platforms[], (.native_packages // {})[], (.downloads // {})[]] |
		 all(.url | type == "string" and startswith("https://dl.reasonix.io/desktop-" + $version + "/")))
	' "$manifest" >/dev/null
	go run "$script_dir/release-site-fetch/main.go" homepage "$version" "$homepage"
	! grep -Eq 'href="[^"]*(tag|download)/desktop-v[0-9]+\.[0-9]+\.[0-9]+' "$homepage"
	go run "$script_dir/release-site-fetch/main.go" changelog "$version" "$changelog"
	grep -Fq "v$version" "$changelog"
	local browser="${CHROME_BIN:-}"
	if [ -z "$browser" ]; then
		for candidate in google-chrome google-chrome-stable chromium chromium-browser; do
			if command -v "$candidate" >/dev/null 2>&1; then browser="$candidate"; break; fi
		done
	fi
	[ -n "$browser" ] || { echo "::error::a Chromium browser is required for hydrated homepage verification" >&2; return 1; }
	# GitHub's default HeadlessChrome identity is challenged by Cloudflare. Use the
	# ordinary Chrome browser identity proven by the protected site probe.
	"$browser" --headless=new --disable-gpu --no-sandbox --virtual-time-budget=10000 \
		--user-agent='Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36' \
		--dump-dom "https://reasonix.io/?download=desktop&release-postflight=v$version" \
		>"$tmp_dir/homepage-hydrated.html"
	grep -Fq "data-release-version=\"desktop\">v$version<" "$tmp_dir/homepage-hydrated.html"
	while IFS= read -r asset; do
		url="$(jq -er --arg asset "$asset" '
			[.platforms[], (.native_packages // {})[], (.downloads // {})[]]
			| map(select(.url | endswith("/" + $asset)))
			| if length == 1 then .[0].url else error("visible asset is missing or ambiguous") end
		' "$manifest")"
		grep -Fq "$url" "$tmp_dir/homepage-hydrated.html"
	done < <(grep -Eo 'data-desktop-asset="[^"]+"' "$homepage" | cut -d '"' -f 2 | sort -u)
}

if [ "$site_only" = "true" ]; then
	verify_site
	node "$script_dir/release-publication-ledger.mjs" site "$version" "$cli_sha" "$operation" \
		"$tmp_dir/desktop-pointer.json" "${ledger_output:-$tmp_dir/site-ledger.json}"
	echo "public site release inputs OK: v$version"
	exit 0
fi

gh release view "$cli_tag" --repo "$repository" --json isDraft,isPrerelease,assets >"$tmp_dir/cli.json"
jq -e '
  .isDraft == false and .isPrerelease == false and
  ([.assets[].name] as $names |
    ["SHA256SUMS", "reasonix-darwin-amd64.tar.gz", "reasonix-darwin-arm64.tar.gz",
     "reasonix-linux-amd64.tar.gz", "reasonix-linux-arm64.tar.gz",
     "reasonix-windows-amd64.zip", "reasonix-windows-arm64.zip"] |
    all(. as $required | $names | index($required)))
' "$tmp_dir/cli.json" >/dev/null

gh release view "$desktop_tag" --repo "$repository" --json isDraft,isPrerelease,assets >"$tmp_dir/desktop.json"
jq -e '
  .isDraft == false and .isPrerelease == false and
  ([.assets[].name] as $names |
    ($names | index("latest.json")) and
    (["Reasonix-darwin-arm64.dmg", "Reasonix-darwin-amd64.dmg",
      "Reasonix-darwin-universal.dmg", "Reasonix-darwin-arm64.zip",
      "Reasonix-darwin-amd64.zip", "Reasonix-linux-amd64.deb",
      "Reasonix-linux-amd64.tar.gz", "Reasonix-windows-amd64-installer.exe",
      "Reasonix-windows-amd64.zip", "Reasonix-windows-arm64-installer.exe"] |
     all(. as $required | ($names | index($required)) and ($names | index($required + ".minisig")))))
' "$tmp_dir/desktop.json" >/dev/null

if [ "${DESKTOP_MANUAL_ONLY:-false}" = "true" ]; then
	bash "$script_dir/manual-desktop-exception.sh" validate "$desktop_tag"
	# Neither updater entry point may serve the manual release: the exception
	# publishes downloads without advancing automatic updates. Assert that
	# invariant rather than one release's prior version.
	gh_latest="$(gh api "repos/$repository/releases/latest" --jq .tag_name)"
	if [ "$gh_latest" = "$desktop_tag" ]; then
		echo "::error::GitHub latest advanced to the manual release $desktop_tag" >&2
		exit 1
	fi
	bash "$script_dir/fetch-stable-release-manifest.sh" "$version" "$tmp_dir/desktop-pointer.json"
	jq -e --arg v "v$version" '.version != $v' "$tmp_dir/desktop-pointer.json" >/dev/null
	gh release view "$desktop_tag" --repo "$repository" --json body --jq .body | grep -F 'manual-download only'
fi

node "$script_dir/release-publication-ledger.mjs" core "$version" "$cli_sha" "$operation" \
	"$tmp_dir/cli.json" "$tmp_dir/desktop.json" "$tmp_dir/core-ledger.json"
if [ "$verify_homepage" = "true" ]; then
	owns_site="$(bash "$script_dir/observe-release-site.sh" "$version" "$operation")"
	if [ "$owns_site" = true ]; then
		verify_site
		node "$script_dir/release-publication-ledger.mjs" site "$version" "$cli_sha" "$operation" \
			"$tmp_dir/desktop-pointer.json" "$tmp_dir/site-ledger.json"
		node "$script_dir/release-publication-ledger.mjs" merge "$tmp_dir/core-ledger.json" \
			"$tmp_dir/site-ledger.json" "${ledger_output:-$tmp_dir/publication-ledger.json}"
	else
		echo "A verified newer Stable release owns the site; recovered immutable v$version files only."
		[ -z "$ledger_output" ] || cp "$tmp_dir/core-ledger.json" "$ledger_output"
	fi
elif [ -n "$ledger_output" ]; then
	cp "$tmp_dir/core-ledger.json" "$ledger_output"
fi
echo "stable release postflight OK: cli=$cli_tag desktop=$desktop_tag"
