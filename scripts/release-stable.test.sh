#!/usr/bin/env bash
set -euo pipefail

repo_root="$(git rev-parse --show-toplevel)"
release_script="$repo_root/scripts/release-stable.sh"
test_root="$(mktemp -d "${TMPDIR:-/tmp}/reasonix-stable-tag-test.XXXXXX")"
cleanup() {
	case "$test_root" in
	*/reasonix-stable-tag-test.*) rm -rf -- "$test_root" ;;
	*) echo "refusing to clean unexpected test directory: $test_root" >&2 ;;
	esac
}
trap cleanup EXIT

mkdir -p "$test_root/bin"
cat >"$test_root/bin/gh" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
if [ -n "${FAKE_ADVANCE_WORK:-}" ] && [ ! -e "$FAKE_ADVANCE_WORK/.advanced-by-fake-gh" ]; then
	: >"$FAKE_ADVANCE_WORK/.advanced-by-fake-gh"
	git -C "$FAKE_ADVANCE_WORK" add .advanced-by-fake-gh
	git -C "$FAKE_ADVANCE_WORK" commit -q -m "advance main during CI"
	git -C "$FAKE_ADVANCE_WORK" push -q origin main-v2
fi
if [ "${FAKE_GH_CONCLUSION:-success}" = pending ]; then
	printf '[{"headSha":"%s","status":"in_progress","conclusion":""}]\n' "$FAKE_CANDIDATE"
else
	printf '[{"headSha":"%s","status":"completed","conclusion":"%s"}]\n' \
		"$FAKE_CANDIDATE" "${FAKE_GH_CONCLUSION:-success}"
fi
EOF
chmod +x "$test_root/bin/gh"

make_remote() {
	local name="$1"
	local work="$test_root/$name-work"
	local remote="$test_root/$name.git"
	git init -q --bare "$remote"
	git init -q -b main-v2 "$work"
	git -C "$work" config user.name test
	git -C "$work" config user.email test@example.invalid
	mkdir -p "$work/release-notes"
	jq '.releases |= map(select(.version != "1.19.2"))' \
		"$repo_root/release-notes/releases.json" >"$work/release-notes/releases.json"
	git -C "$work" add release-notes/releases.json
	git -C "$work" commit -q -m base
	cp "$repo_root/release-notes/releases.json" "$work/release-notes/releases.json"
	git -C "$work" add release-notes/releases.json
	git -C "$work" commit -q -m "reviewed release notes"
	git -C "$work" remote add origin "$remote"
	git -C "$work" push -q origin main-v2
	printf '%s\n' "$work"
}

success_work="$(make_remote success)"
success_sha="$(git -C "$success_work" rev-parse HEAD)"
(
	cd "$success_work"
	PATH="$test_root/bin:$PATH" FAKE_CANDIDATE="$success_sha" \
		RELEASE_CI_WAIT_SECONDS=0 RELEASE_REMOTE=origin \
		"$release_script" 1.19.2
)
for tag in v1.19.2 npm-v1.19.2 desktop-v1.19.2; do
	[ "$(git ls-remote --tags --refs "$test_root/success.git" "refs/tags/$tag" | awk 'NR == 1 { print $1 }')" = "$success_sha" ]
done

# Advancing main after the atomic tag transaction must not invalidate the
# immutable Notes candidate selected by the protected relay.
git clone -q -b main-v2 "$test_root/success.git" "$test_root/success-control"
git -C "$test_root/success-control" config user.name test
git -C "$test_root/success-control" config user.email test@example.invalid
git -C "$test_root/success-control" commit --allow-empty -q -m "advance after tags"
git -C "$test_root/success-control" push -q origin main-v2
(
	cd "$test_root/success-control"
	GITHUB_OUTPUT="$test_root/success-control.out" RELEASE_REMOTE=origin \
		RELEASE_TAG=v1.19.2 ALLOW_STABLE_RECOVERY=false \
		"$repo_root/scripts/resolve-stable-release.sh"
)
grep -Eq '^sha='"$success_sha"'$' "$test_root/success-control.out"
(
	cd "$success_work"
	GITHUB_OUTPUT="$test_root/stale-control.out" RELEASE_REMOTE=origin \
		RELEASE_TAG=v1.19.2 ALLOW_STABLE_RECOVERY=false \
		"$repo_root/scripts/resolve-stable-release.sh"
)
grep -Eq '^sha='"$success_sha"'$' "$test_root/stale-control.out"

failure_work="$(make_remote failure)"
failure_sha="$(git -C "$failure_work" rev-parse HEAD)"
if (
	cd "$failure_work"
	PATH="$test_root/bin:$PATH" FAKE_CANDIDATE="$failure_sha" FAKE_GH_CONCLUSION=failure \
		RELEASE_CI_WAIT_SECONDS=0 RELEASE_REMOTE=origin \
		"$release_script" 1.19.2
); then
	echo "failed CI unexpectedly created release tags" >&2
	exit 1
fi
[ -z "$(git ls-remote --tags --refs "$test_root/failure.git" 'refs/tags/*')" ]

# A later code commit is not the reviewed Notes merge and must never become the
# release candidate merely because it contains the older reviewed record.
stale_notes_work="$(make_remote stale-notes)"
git -C "$stale_notes_work" commit --allow-empty -q -m "later product change"
git -C "$stale_notes_work" push -q origin main-v2
stale_notes_sha="$(git -C "$stale_notes_work" rev-parse HEAD)"
if (
	cd "$stale_notes_work"
	PATH="$test_root/bin:$PATH" FAKE_CANDIDATE="$stale_notes_sha" \
		RELEASE_CI_WAIT_SECONDS=0 RELEASE_REMOTE=origin \
		"$release_script" 1.19.2
); then
	echo "candidate after the reviewed Notes commit unexpectedly created release tags" >&2
	exit 1
fi
[ -z "$(git ls-remote --tags --refs "$test_root/stale-notes.git" 'refs/tags/*')" ]

# If main advances while exact-SHA CI is being checked, the no-op main-v2
# refspec makes the atomic push reject all three tags.
race_work="$(make_remote race)"
race_sha="$(git -C "$race_work" rev-parse HEAD)"
git clone -q -b main-v2 "$test_root/race.git" "$test_root/race-advance"
git -C "$test_root/race-advance" config user.name test
git -C "$test_root/race-advance" config user.email test@example.invalid
if (
	cd "$race_work"
	PATH="$test_root/bin:$PATH" FAKE_CANDIDATE="$race_sha" \
		FAKE_ADVANCE_WORK="$test_root/race-advance" RELEASE_CI_WAIT_SECONDS=0 \
		RELEASE_REMOTE=origin "$release_script" 1.19.2
); then
	echo "main-v2 advance during CI unexpectedly created release tags" >&2
	exit 1
fi
[ -z "$(git ls-remote --tags --refs "$test_root/race.git" 'refs/tags/*')" ]
[ "$(git ls-remote "$test_root/race.git" refs/heads/main-v2 | awk 'NR == 1 { print $1 }')" != "$race_sha" ]

occupied_work="$(make_remote occupied)"
occupied_sha="$(git -C "$occupied_work" rev-parse HEAD)"
git -C "$occupied_work" tag v1.19.2
git -C "$occupied_work" push -q origin v1.19.2
if (
	cd "$occupied_work"
	PATH="$test_root/bin:$PATH" FAKE_CANDIDATE="$occupied_sha" \
		RELEASE_CI_WAIT_SECONDS=0 RELEASE_REMOTE=origin \
		"$release_script" 1.19.2
); then
	echo "occupied release identity unexpectedly passed" >&2
	exit 1
fi
[ -z "$(git ls-remote --tags --refs "$test_root/occupied.git" refs/tags/npm-v1.19.2 refs/tags/desktop-v1.19.2)" ]

if "$release_script" 1.19.2-preview.1 >/dev/null 2>&1; then
	echo "Preview version unexpectedly passed the Stable tag helper" >&2
	exit 1
fi

echo "stable release tag helper tests: PASS"
