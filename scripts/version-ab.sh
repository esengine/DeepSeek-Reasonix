#!/usr/bin/env bash
# Compare two Reasonix versions on the committed e2e suite.
#
# The harness, the task set and the grader all come from the working tree; only
# the agent binary differs between arms. What makes this experiment fragile is
# that an older binary reads its configuration differently — v1.0.0 has no
# --auto flag, ignores REASONIX_HOME, and resolves the API key from the process
# environment rather than the credential store. Each of those produces a
# quietly different experiment instead of a failure, so they are asserted here
# rather than discovered halfway through a run.
#
# Usage:
#   scripts/version-ab.sh <old-ref> [-model NAME] [-tasks IDS] [-out DIR]
#
#   e.g. scripts/version-ab.sh v1.0.0 -model deepseek-flash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OLD_REF="${1:-}"
[ -n "$OLD_REF" ] || { echo "usage: $0 <old-ref> [-model NAME] [-tasks IDS] [-out DIR]" >&2; exit 2; }
shift

MODEL=""
TASKS=""
OUT="${TMPDIR:-/tmp}/reasonix-ab-$(echo "$OLD_REF" | tr -c 'A-Za-z0-9._-' '_')"
while [ $# -gt 0 ]; do
	case "$1" in
	-model) MODEL="$2"; shift 2 ;;
	-tasks) TASKS="$2"; shift 2 ;;
	-out) OUT="$2"; shift 2 ;;
	*) echo "unknown flag: $1" >&2; exit 2 ;;
	esac
done

# A default model would be resolved separately by each binary, and two versions
# do not agree on which provider that is — one arm ends up on a different
# endpoint, which is a different experiment wearing the same name.
[ -n "$MODEL" ] || { echo "-model is required: both arms must address one provider" >&2; exit 2; }

git -C "$ROOT" rev-parse --verify "$OLD_REF^{commit}" >/dev/null 2>&1 ||
	{ echo "$OLD_REF is not a commit in this repository" >&2; exit 1; }

mkdir -p "$OUT"
WORKTREE="$OUT/src"
OLD_BIN="$OUT/reasonix-old"
NEW_BIN="$OUT/reasonix-new"

echo "==> building $OLD_REF"
[ -d "$WORKTREE" ] || git -C "$ROOT" worktree add --detach "$WORKTREE" "$OLD_REF" >/dev/null
(cd "$WORKTREE" && go build -o "$OLD_BIN" ./cmd/reasonix)

echo "==> building working tree"
(cd "$ROOT" && go build -o "$NEW_BIN" ./cmd/reasonix)

# e2ebench always passes --auto. A version that predates the flag already runs
# tools without asking, so the shim drops it rather than downgrading the arm to
# an interactive posture it would fail in.
OLD_DRIVER="$OLD_BIN"
if ! "$OLD_BIN" run --help 2>&1 | grep -q -- "-auto"; then
	OLD_DRIVER="$OUT/old-shim"
	cat > "$OLD_DRIVER" <<-SHIM
		#!/usr/bin/env bash
		args=()
		for a in "\$@"; do [ "\$a" = "--auto" ] || args+=("\$a"); done
		exec "$OLD_BIN" "\${args[@]}"
	SHIM
	chmod +x "$OLD_DRIVER"
	echo "==> $OLD_REF predates --auto; driving it through a shim"
fi

# One provider, one endpoint, one price list. Currency is what the run reports
# about the provider it actually reached, so two arms disagreeing on it means
# they were never comparable — the failure this asserts against is silent.
smoke() {
	local bin="$1" out="$2" log="$2.log"
	rm -f "$out"
	# Run from a scratch directory, not the repo: a working directory carries
	# .env files and convention dirs into configuration resolution, and the two
	# versions do not read the same config files, so the repo root can offer
	# each arm a different provider set.
	(cd "$OUT" && "$bin" run --auto --model "$MODEL" --metrics "$out" "Reply with exactly: ok") >"$log" 2>&1 || true
	[ -s "$out" ] || {
		echo "smoke run produced no metrics: $bin" >&2
		sed -n '1,10p' "$log" >&2
		exit 1
	}
	# Normalise the symbol: versions differ on ¥ vs CNY for one currency, and a
	# spelling difference must not read as a different provider.
	python3 -c "
import json
sym = {'¥': 'CNY', '\$': 'USD', '€': 'EUR'}
c = (json.load(open('$out')).get('currency') or '').strip()
print(sym.get(c, c.upper()))"
}

echo "==> smoke: verifying both arms reach the same provider"
old_currency="$(smoke "$OLD_DRIVER" "$OUT/smoke-old.json")"
new_currency="$(smoke "$NEW_BIN" "$OUT/smoke-new.json")"
if [ "$old_currency" != "$new_currency" ]; then
	echo "arms reached different providers (currency $old_currency vs $new_currency);" >&2
	echo "check that both resolve $MODEL and that each finds its API key:" >&2
	echo "  older versions read the process environment; current ones read \$REASONIX_HOME/.env" >&2
	exit 1
fi
echo "    both on $new_currency"

task_args=()
[ -n "$TASKS" ] && task_args=(-task "$TASKS")

for arm in old new; do
	bin="$OLD_DRIVER"; [ "$arm" = new ] && bin="$NEW_BIN"
	echo "==> arm: $arm"
	(cd "$ROOT" && go run ./cmd/e2ebench -bin "$bin" -model "$MODEL" -budget 0 \
		"${task_args[@]}" -json "$OUT/arm-$arm.json" -out "$OUT/arm-$arm.md" >/dev/null)
done

echo "==> comparison"
(cd "$ROOT" && go run ./cmd/e2ebench -mode compare "$OUT/arm-old.json" "$OUT/arm-new.json")
echo
echo "reports: $OUT/arm-old.md  $OUT/arm-new.md"
echo "remove the worktree with: git worktree remove $WORKTREE"
