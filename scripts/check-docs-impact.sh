#!/usr/bin/env bash
set -euo pipefail

body="${DOCS_IMPACT_PR_BODY:-${PR_BODY:-}}"
if [ -n "${DOCS_IMPACT_PR_BODY_FILE:-}" ]; then
	body="$(<"$DOCS_IMPACT_PR_BODY_FILE")"
fi

changed_input=""
if [ "$#" -gt 0 ]; then
	changed_input="$(printf '%s\n' "$@")"
elif [ -n "${DOCS_IMPACT_CHANGED_FILES_FILE:-}" ]; then
	changed_input="$(<"$DOCS_IMPACT_CHANGED_FILES_FILE")"
elif [ -n "${DOCS_IMPACT_CHANGED_FILES:-}" ]; then
	changed_input="$DOCS_IMPACT_CHANGED_FILES"
else
	base="${DOCS_IMPACT_BASE_SHA:-${BASE_SHA:-}}"
	head="${DOCS_IMPACT_HEAD_SHA:-${HEAD_SHA:-HEAD}}"
	if [ -z "$base" ]; then
		base="$(git merge-base origin/main-v2 "$head" 2>/dev/null || git merge-base main-v2 "$head")"
	fi
	diff_base="$base"
	if merge_base="$(git merge-base "$base" "$head" 2>/dev/null)"; then
		diff_base="$merge_base"
	fi
	changed_input="$(git diff --name-only "$diff_base" "$head")"
fi

user_visible=()
docs_changed=()
while IFS= read -r file; do
	[ -z "$file" ] && continue
	case "$file" in
		docs/*.md) docs_changed+=("$file") ;;
	esac
	case "$file" in
		*_test.go|*.test.*|*.spec.*|*/__tests__/*|*/go.mod|*/go.sum|go.mod|go.sum|*/pnpm-lock.yaml|*/package-lock.json)
			continue
			;;
	esac
	case "$file" in
		cmd/reasonix/*|\
		desktop/*|\
		internal/agent/*|\
		internal/boot/*|\
		internal/cli/*|\
		internal/config/*|\
		internal/control/*|\
		internal/history/*|\
		internal/memory/*|\
		internal/permission/*|\
		internal/plugin/*|\
		internal/productdocs/*|\
		internal/provider/*|\
		internal/recovery/*|\
		internal/remote/*|\
		internal/sandbox/*|\
		internal/serve/*|\
		internal/skill/*|\
		internal/tool/*|\
		npm/*|\
		.goreleaser.yaml|\
		scripts/desktop-build.sh)
			user_visible+=("$file")
			;;
	esac
done <<< "$changed_input"

if [ "${#user_visible[@]}" -eq 0 ]; then
	echo "No user-visible product files changed; documentation impact check skipped."
	exit 0
fi

line="$(printf '%s\n' "$body" | grep -Eim1 '^[[:space:]>#*_-]*Documentation-impact[[:space:]]*:' || true)"
value="${line#*:}"
value="${value#"${value%%[![:space:]]*}"}"
lower="$(printf '%s' "$value" | tr '[:upper:]' '[:lower:]')"

fail() {
	echo "Documentation impact check failed: $1" >&2
	echo "User-visible files:" >&2
	printf '  - %s\n' "${user_visible[@]}" >&2
	if [ "${#docs_changed[@]}" -gt 0 ]; then
		echo "Embedded documentation files:" >&2
		printf '  - %s\n' "${docs_changed[@]}" >&2
	fi
	echo "Required PR field:" >&2
	echo "  Documentation-impact: updated - <what changed>" >&2
	echo "  Documentation-impact: none - <why existing docs remain correct>" >&2
	exit 1
}

[ -n "$line" ] || fail "missing Documentation-impact field"
if [ "${#docs_changed[@]}" -gt 0 ]; then
	[[ "$lower" =~ ^updated[[:space:]]*[-:][[:space:]]*.+$ ]] || fail "docs/*.md changed, so Documentation-impact must explain what was updated"
else
	[[ "$lower" =~ ^none[[:space:]]*[-:][[:space:]]*.+$ ]] || fail "no docs/*.md changed, so Documentation-impact must explain why none is required"
fi

echo "Documentation impact check passed: $value"
