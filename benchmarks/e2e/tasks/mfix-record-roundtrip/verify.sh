#!/usr/bin/env bash
set -e
# Fresh bytecode cache: macOS system Python caches centrally (pycache_prefix),
# where a same-size same-second edit is invisible; a throwaway prefix defeats it.
export PYTHONPYCACHEPREFIX="$(mktemp -d)"
find . -name '__pycache__' -type d -prune -exec rm -rf {} +
python3 - <<'EOF'
import tempfile

from reader import load
from writer import dump

records = [("ann", 3), ("bo", 5), ("cyrus", 0)]
with tempfile.NamedTemporaryFile(suffix=".txt", delete=False) as f:
    path = f.name
dump(records, path)
assert load(path) == records, load(path)

dump([], path)
assert load(path) == []
EOF
