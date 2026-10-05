#!/usr/bin/env bash
# Publish one Studio release's CLI archives as a CLI release the installed
# updaters accept, then advance the public pointer for its channel.
#
# The updater shipped in 1.x reads crash.reasonix.io/v1/cli/releases/stable/latest.json
# and refuses any release whose tag is not vMAJOR.MINOR.PATCH or whose asset URLs are
# not github.com/<repo>/releases/download/<that same tag>/<name>. The archives
# therefore live in a GitHub release named v<version> beside the studio-v<version>
# one, and the manifest is built from that release exactly as the 1.x line builds it.
#
# Order matters: GitHub release, then the immutable per-tag record, then the
# mutable pointer. A rerun that finds any of them already there checks it against
# its own contents and keeps it; archives are not byte-reproducible, so an
# existing release is never compared with a rebuild.
#
# The 1.x line writes the same pointer under a different lock, so this only runs
# while it is frozen.
set -euo pipefail

script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"

version="${CLI_VERSION:?CLI_VERSION is required (for example v2.24.0)}"
repository="${REPOSITORY:?REPOSITORY is required}"
archives="${ARCHIVES_DIR:?ARCHIVES_DIR is required}"
bucket="${R2_BUCKET:?R2_BUCKET is required}"
studio_tag="${STUDIO_TAG:?STUDIO_TAG is required}"
approved_sha="${APPROVED_SHA:?APPROVED_SHA is required}"
[ "${CLI_PUBLISH_FROZEN:-}" = true ] || {
	echo "::error::CLI_PUBLISH_FROZEN must be true: the 1.x line still owns the CLI update pointer"
	exit 1
}
endpoint="${R2_ENDPOINT:-https://${R2_ACCOUNT_ID:?R2_ACCOUNT_ID or R2_ENDPOINT is required}.r2.cloudflarestorage.com}"

tag="v${version#v}"
stable_re='^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$'
preview_re='^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)-preview\.(0|[1-9][0-9]*)$'
if [[ "$tag" =~ $stable_re ]]; then
	channel=stable
	prerelease=false
elif [[ "$tag" =~ $preview_re ]]; then
	channel=preview
	prerelease=true
else
	echo "::notice::$tag is neither a stable nor a -preview.N version; no CLI release or pointer is published for it"
	exit 0
fi

# The cli-tag job creates the v* tag before this runs; it has to be
# there already, on the commit the studio release was built from.
RELEASE_TAG="$tag" APPROVED_SHA="$approved_sha" VERIFY_RELEASE_CHECKOUT=false \
	bash "$script_dir/verify-release-tag.sh"

assets=(
	reasonix-darwin-amd64.tar.gz
	reasonix-darwin-arm64.tar.gz
	reasonix-linux-amd64.tar.gz
	reasonix-linux-arm64.tar.gz
	reasonix-windows-amd64.zip
	reasonix-windows-arm64.zip
	SHA256SUMS
)

work="$(mktemp -d)"
trap 'rm -rf "$work"' EXIT
not_found='404|NoSuchKey|Not Found'

digest_of() { shasum -a 256 "$1" | awk '{print $1}'; }

# The digest list has to describe the very bytes about to be uploaded.
for name in "${assets[@]}"; do
	[ -f "$archives/$name" ] || { echo "::error::no $archives/$name"; exit 1; }
done
[ "$(grep -c '' "$archives/SHA256SUMS")" -eq 6 ] || {
	echo "::error::SHA256SUMS must list exactly the six archives"
	exit 1
}
for name in "${assets[@]}"; do
	[ "$name" = SHA256SUMS ] && continue
	grep -Fxq "$(digest_of "$archives/$name")  $name" "$archives/SHA256SUMS" || {
		echo "::error::SHA256SUMS does not match $name"
		exit 1
	}
done

release_json="$work/release.json"
release_error="$work/release.error"
checksums="$work/SHA256SUMS"
if gh api "repos/$repository/releases/tags/$tag" >"$release_json" 2>"$release_error"; then
	gh release download "$tag" -R "$repository" --pattern SHA256SUMS --output "$checksums"
	decision="$(bash "$script_dir/decide-cli-release-publication.sh" "$channel" "$tag" "$repository" "$release_json" "$checksums")"
elif grep -Eiq "$not_found" "$release_error"; then
	decision=publish
else
	cat "$release_error" >&2
	exit 1
fi

if [ "$decision" = publish ]; then
	flags=(--latest=false)
	[ "$prerelease" = true ] && flags+=(--prerelease)
	# --verify-tag: a missing tag is an error here, never something to create.
	files=()
	for name in "${assets[@]}"; do files+=("$archives/$name"); done
	gh release create "$tag" "${files[@]}" -R "$repository" --verify-tag "${flags[@]}" \
		--title "Reasonix CLI $tag" \
		--notes "CLI archives built from the Studio release [$studio_tag](https://github.com/$repository/releases/tag/$studio_tag)."
	gh api "repos/$repository/releases/tags/$tag" >"$release_json"
	gh release download "$tag" -R "$repository" --pattern SHA256SUMS --output "$checksums"
	[ "$(bash "$script_dir/decide-cli-release-publication.sh" "$channel" "$tag" "$repository" "$release_json" "$checksums")" = reuse ] || {
		echo "::error::the CLI release $tag was created but does not verify"
		exit 1
	}
fi

manifest="$work/manifest.json"
jq --arg tag "$tag" --argjson names "$(printf '%s\n' "${assets[@]}" | jq -R . | jq -sc .)" '
	. as $release |
	($release.assets | map({key: .name, value: .}) | from_entries) as $by_name |
	{
		tag_name: $release.tag_name,
		prerelease: $release.prerelease,
		html_url: $release.html_url,
		release_notes_url: null,
		assets: [$names[] as $name | $by_name[$name] | {name, browser_download_url, size}]
	}
' "$release_json" >"$manifest"
bash "$script_dir/validate-cli-release-manifest.sh" "legacy-$channel" "$tag" "$repository" "$manifest" "$tag"

json_type=(--content-type "application/json; charset=utf-8")
immutable_key="cli/releases/$tag/latest.json"
immutable="$work/immutable.json"
if aws s3 cp "s3://$bucket/$immutable_key" "$immutable" --endpoint-url "$endpoint" 2>"$work/immutable.error"; then
	bash "$script_dir/validate-cli-release-manifest.sh" "legacy-$channel" "$tag" "$repository" "$immutable" "$tag"
	bash "$script_dir/compare-cli-release-manifests.sh" "$manifest" "$immutable" || {
		echo "::error::immutable CLI release metadata for $tag already exists with different content"
		exit 1
	}
	echo "immutable CLI release metadata for $tag already exists; preserving it"
elif grep -Eiq "$not_found" "$work/immutable.error"; then
	aws s3 cp "$manifest" "s3://$bucket/$immutable_key" --endpoint-url "$endpoint" "${json_type[@]}" \
		--cache-control "public, max-age=31536000, immutable"
else
	cat "$work/immutable.error" >&2
	exit 1
fi
aws s3 cp "s3://$bucket/$immutable_key" "$immutable" --endpoint-url "$endpoint"
bash "$script_dir/validate-cli-release-manifest.sh" "legacy-$channel" "$tag" "$repository" "$immutable" "$tag"
bash "$script_dir/compare-cli-release-manifests.sh" "$manifest" "$immutable"

pointer_key="cli/$channel/latest.json"
pointer="$work/pointer.json"
current=-
if aws s3 cp "s3://$bucket/$pointer_key" "$pointer" --endpoint-url "$endpoint" 2>"$work/pointer.error"; then
	current_tag="$(jq -er '.tag_name | strings' "$pointer")"
	bash "$script_dir/validate-cli-release-manifest.sh" "legacy-$channel" "$current_tag" "$repository" "$pointer" "$current_tag"
	current="$pointer"
elif ! grep -Eiq "$not_found" "$work/pointer.error"; then
	cat "$work/pointer.error" >&2
	exit 1
fi

if [ "$(bash "$script_dir/decide-cli-pointer-update.sh" "$channel" "$manifest" "$current")" = skip ]; then
	echo "CLI $channel pointer stays; $tag is not newer and needs no repair"
	exit 0
fi
aws s3 cp "$manifest" "s3://$bucket/$pointer_key" --endpoint-url "$endpoint" "${json_type[@]}" \
	--cache-control "public, max-age=300, stale-if-error=86400"
aws s3 cp "s3://$bucket/$pointer_key" "$pointer" --endpoint-url "$endpoint"
bash "$script_dir/validate-cli-release-manifest.sh" "legacy-$channel" "$tag" "$repository" "$pointer" "$tag"
if ! cmp -s "$manifest" "$pointer"; then
	# Another writer got in between our write and this read; only a newer pointer is acceptable.
	if [ "$(bash "$script_dir/decide-cli-pointer-update.sh" "$channel" "$manifest" "$pointer")" != skip ]; then
		echo "::error::the CLI $channel pointer was replaced by an older or different manifest after this run wrote $tag"
		exit 1
	fi
	echo "::warning::the CLI $channel pointer moved past $tag while this run was writing it"
	exit 0
fi
echo "CLI $channel pointer -> $tag"
