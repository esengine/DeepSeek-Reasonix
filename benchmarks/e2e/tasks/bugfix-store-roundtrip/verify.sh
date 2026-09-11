#!/usr/bin/env bash
set -e
# Fresh bytecode cache: macOS system Python caches centrally (pycache_prefix),
# where a same-size same-second edit is invisible; a throwaway prefix defeats it.
export PYTHONPYCACHEPREFIX="$(mktemp -d)"
find . -name '__pycache__' -type d -prune -exec rm -rf {} +
python3 - <<'EOF'
from store import Store

s = Store()
s.set("Alpha", 1)
assert s.get("Alpha") == 1, "set/get must round-trip the same key"
s.set("beta", 2)
assert s.get("beta") == 2
s.set("Alpha", 3)
assert s.get("Alpha") == 3, "overwrite must be visible"
assert len(s) == 2, "same key set twice must not duplicate"
s.delete("Alpha")
assert s.get("Alpha") is None, "delete must remove what set stored"
EOF
