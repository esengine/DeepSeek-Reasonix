#!/usr/bin/env bash
set -e
# Fresh bytecode cache: macOS system Python caches centrally (pycache_prefix),
# where a same-size same-second edit is invisible; a throwaway prefix defeats it.
export PYTHONPYCACHEPREFIX="$(mktemp -d)"
find . -name '__pycache__' -type d -prune -exec rm -rf {} +
python3 - <<'EOF'
from parser import parse_all

want = [
    ("fen", 3),
    ("gorse", 12),
    ("heath", 7),
    ("moor", 1),
    ("tor", 22),
    ("vale", 5),
]
got = parse_all("fixtures")
assert got == want, f"parse_all = {got}, want {want}"
EOF
