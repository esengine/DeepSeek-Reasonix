#!/usr/bin/env bash
# Measure one desktop shell build: launch with a disposable data home, wait for
# the Go lifecycle diagnostics to reach "healthy", then sample the full process
# tree (shell, renderer/GPU helpers and the Go service) at fixed offsets.
# Output is one JSON document; compare runs of this script only, never against
# a differently sampled figure. The retired Wails shell's baseline runs live in
# docs/desktop-migration/baseline/.
#
# Usage: scripts/desktop-shell-metrics.sh <executable> <label> <out.json> [idle_seconds]
#   executable: the packaged app's main executable
#   label:      free text recorded in the output (e.g. electron-darwin-arm64)
set -euo pipefail

exe="${1:?usage: desktop-shell-metrics.sh <executable> <label> <out.json> [idle_seconds]}"
label="${2:?label}"
out="${3:?out.json}"
idle="${4:-30}"

home="$(mktemp -d "${TMPDIR:-/tmp}/reasonix-shell-metrics.XXXXXX")"
cleanup() {
	if [ -n "${pid:-}" ] && kill -0 "$pid" 2>/dev/null; then
		kill -TERM "$pid" 2>/dev/null || true
		for _ in $(seq 1 50); do
			kill -0 "$pid" 2>/dev/null || break
			sleep 0.1
		done
		kill -KILL "$pid" 2>/dev/null || true
	fi
	rm -rf "$home"
}
trap cleanup EXIT

now_ms() { python3 -c 'import time; print(int(time.time()*1000))'; }

# Process-tree RSS in KiB: the launched pid, its descendants, and helper
# processes that started after launch and belong to the same shell family
# (WebKit XPC helpers on macOS re-parent to launchd; Electron helpers do not).
tree_rss() {
	python3 - "$pid" "$launch_epoch" <<'PY'
import subprocess, sys, time
root = int(sys.argv[1]); launched = float(sys.argv[2])
rows = subprocess.run(["ps", "-axo", "pid=,ppid=,rss=,lstart=,comm="], capture_output=True, text=True).stdout.splitlines()
procs = {}
for row in rows:
    parts = row.split(None, 3)
    if len(parts) < 4:
        continue
    pid, ppid, rss = int(parts[0]), int(parts[1]), int(parts[2])
    rest = parts[3]
    # lstart is 5 whitespace-separated fields; the remainder is comm.
    fields = rest.split(None, 5)
    if len(fields) < 6:
        continue
    started = " ".join(fields[:5]); comm = fields[5]
    try:
        epoch = time.mktime(time.strptime(started, "%a %b %d %H:%M:%S %Y"))
    except ValueError:
        epoch = 0
    procs[pid] = (ppid, rss, epoch, comm)
family = set()
def descend(p):
    for q, (pp, _, _, _) in procs.items():
        if pp == p and q not in family:
            family.add(q); descend(q)
if root in procs:
    family.add(root); descend(root)
helpers = ("WebKit", "Reasonix Helper", "reasonix-desktop", "Electron Helper", "reasonix", "chrome_crashpad")
for p, (pp, rss, epoch, comm) in procs.items():
    if p in family:
        continue
    if epoch >= launched - 1 and any(h in comm for h in helpers):
        family.add(p)
total = sum(procs[p][1] for p in family)
print(total, len(family), ";".join(sorted({procs[p][3].rsplit('/',1)[-1] for p in family})))
PY
}

lifecycle_phase() {
	python3 - "$home" "$pid" <<'PY'
import glob, json, os, sys
home, pid = sys.argv[1], sys.argv[2]
# The Go process writes the file under its own pid; a fresh home has one.
paths = glob.glob(os.path.join(home, "**", "diagnostics", "lifecycle", "*.json"), recursive=True)
for path in sorted(paths, key=os.path.getmtime, reverse=True):
    try:
        with open(path) as fh:
            state = json.load(fh)
        print(state.get("phase", ""), state.get("startedAt", ""), state.get("updatedAt", ""))
        break
    except (OSError, ValueError):
        pass
PY
}

export REASONIX_HOME="$home" REASONIX_STATE_HOME="$home" REASONIX_CACHE_HOME="$home/cache" REASONIX_DEV=1
launch_epoch="$(python3 -c 'import time; print(time.time())')"
t0="$(now_ms)"
"$exe" >"$home/stdout.log" 2>"$home/stderr.log" &
pid=$!

phase=""; healthy_ms=""; ready_ms=""
for _ in $(seq 1 600); do
	sleep 0.1
	kill -0 "$pid" 2>/dev/null || { echo "shell exited before becoming healthy" >&2; cat "$home/stderr.log" >&2; exit 1; }
	read -r phase _ _ < <(lifecycle_phase) || true
	case "$phase" in
	ready) [ -n "$ready_ms" ] || ready_ms=$(( $(now_ms) - t0 )) ;;
	healthy) [ -n "$ready_ms" ] || ready_ms=$(( $(now_ms) - t0 )); healthy_ms=$(( $(now_ms) - t0 )); break ;;
	esac
done
[ -n "$healthy_ms" ] || { echo "shell never reported healthy (last phase: '$phase')" >&2; exit 1; }

samples="["
for offset in 2 10 "$idle"; do
	sleep "$offset"
	read -r rss_kib count names < <(tree_rss)
	samples+="{\"afterHealthySeconds\":$offset,\"rssKiB\":$rss_kib,\"processCount\":$count,\"processes\":\"$names\"},"
done
samples="${samples%,}]"

kill -TERM "$pid"
exit_t0="$(now_ms)"
for _ in $(seq 1 100); do
	kill -0 "$pid" 2>/dev/null || break
	sleep 0.1
done
exit_ms=$(( $(now_ms) - exit_t0 ))
kill -0 "$pid" 2>/dev/null && exit_clean=false || exit_clean=true

python3 - "$out" "$label" "$exe" "$ready_ms" "$healthy_ms" "$samples" "$exit_ms" "$exit_clean" <<'PY'
import json, platform, sys, datetime
out, label, exe, ready, healthy, samples, exit_ms, exit_clean = sys.argv[1:]
doc = {
    "label": label, "executable": exe,
    "measuredAt": datetime.datetime.now(datetime.timezone.utc).isoformat(),
    "machine": {"platform": platform.platform(), "machine": platform.machine()},
    "startup": {"readyMs": int(ready), "healthyMs": int(healthy)},
    "samples": json.loads(samples),
    "exit": {"terminatedWithinMs": int(exit_ms), "clean": exit_clean == "true"},
}
with open(out, "w") as fh:
    json.dump(doc, fh, indent=2); fh.write("\n")
print(json.dumps(doc, indent=2))
PY
