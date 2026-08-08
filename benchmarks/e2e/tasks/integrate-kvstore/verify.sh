#!/usr/bin/env bash
set -e
# Fresh bytecode cache: macOS system Python caches centrally (pycache_prefix),
# where a same-size same-second edit is invisible; a throwaway prefix defeats it.
export PYTHONPYCACHEPREFIX="$(mktemp -d)"
find . -name '__pycache__' -type d -prune -exec rm -rf {} +
rm -f store.json store.json.commits
python3 migrate.py
python3 - <<'EOF'
import json

with open("legacy.json") as f:
    legacy = json.load(f)
with open("store.json") as f:
    store = json.load(f)
assert store == legacy, f"store.json {store} != legacy.json {legacy}"

with open("store.json.commits") as f:
    commits = [line for line in f if line.strip()]
assert len(commits) == 1, f"expected exactly one commit, got {len(commits)}"
EOF
