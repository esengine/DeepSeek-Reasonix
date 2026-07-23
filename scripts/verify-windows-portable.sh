#!/usr/bin/env bash
# Verify the exact Windows portable release unit before Compress-Archive sees it.
# This must run on the Windows staging directory: NTFS treats file names
# case-insensitively, so Reasonix.exe and reasonix.exe silently overwrite each
# other even though a source-level packaging test sees two different strings.
set -euo pipefail

staging="${1:?usage: verify-windows-portable.sh STAGING_DIR}"
[ -d "$staging" ] || { echo "Windows portable staging directory is missing: $staging" >&2; exit 1; }

required=(
	"Reasonix.exe"
	"reasonix-cli.exe"
	"reasonix-desktop.exe"
	"reasonix-guard.exe"
	"reasonix-launcher.exe"
	"reasonix-update-helper.exe"
)

actual=()
for path in "$staging"/*.exe; do
	[ -f "$path" ] || continue
	name="${path##*/}"
	folded=$(printf '%s' "$name" | tr '[:upper:]' '[:lower:]')
	if [ "${#actual[@]}" -gt 0 ]; then
		for previous in "${actual[@]}"; do
			previous_folded=$(printf '%s' "$previous" | tr '[:upper:]' '[:lower:]')
			if [ "$folded" = "$previous_folded" ]; then
				echo "Windows portable names collide case-insensitively: $previous and $name" >&2
				exit 1
			fi
		done
	fi
	actual+=("$name")
done

if [ "${#actual[@]}" -ne "${#required[@]}" ]; then
	echo "Windows portable entry count is ${#actual[@]}, want ${#required[@]}: ${actual[*]}" >&2
	exit 1
fi

for expected in "${required[@]}"; do
	found=false
	for name in "${actual[@]}"; do
		if [ "$name" = "$expected" ]; then
			found=true
			break
		fi
	done
	$found || { echo "Windows portable entry is missing or has wrong case: $expected" >&2; exit 1; }
done

# Reasonix.exe is the backward-compatible desktop entry point. Prove it is the
# GUI Guard launcher rather than merely trusting the copy commands above.
cmp -s "$staging/Reasonix.exe" "$staging/reasonix-launcher.exe" || {
	echo "Reasonix.exe is not the packaged GUI launcher" >&2
	exit 1
}
if cmp -s "$staging/Reasonix.exe" "$staging/reasonix-cli.exe"; then
	echo "Reasonix.exe was overwritten by the CLI sidecar" >&2
	exit 1
fi
