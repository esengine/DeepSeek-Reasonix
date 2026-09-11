#!/usr/bin/env bash
set -e
# Fresh bytecode cache: macOS system Python caches centrally (pycache_prefix),
# where a same-size same-second edit is invisible; a throwaway prefix defeats it.
export PYTHONPYCACHEPREFIX="$(mktemp -d)"
find . -name '__pycache__' -type d -prune -exec rm -rf {} +
python3 - <<'EOF'
from utils import head, tail, chunk

assert tail([1, 2, 3], 2) == [2, 3], tail([1, 2, 3], 2)
assert tail([1, 2, 3], 1) == [3], tail([1, 2, 3], 1)
assert tail([7], 1) == [7], tail([7], 1)
assert tail([1, 2], 5) == [1, 2], tail([1, 2], 5)
assert tail([1, 2, 3], 0) == []
assert head([1, 2, 3], 2) == [1, 2]
assert chunk([1, 2, 3, 4], 2) == [[1, 2], [3, 4]]
EOF
