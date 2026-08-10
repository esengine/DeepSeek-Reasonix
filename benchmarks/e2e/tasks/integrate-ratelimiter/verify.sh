#!/usr/bin/env bash
set -e
# Fresh bytecode cache: macOS system Python caches centrally (pycache_prefix),
# where a same-size same-second edit is invisible; a throwaway prefix defeats it.
export PYTHONPYCACHEPREFIX="$(mktemp -d)"
find . -name '__pycache__' -type d -prune -exec rm -rf {} +
python3 - <<'EOF'
import inspect

import throttle

events = [0, 0, 0, 0, 0.5, 1.0, 1.2, 2.0, 10, 10.1, 10.2, 10.3, 10.4]
want = [0, 0, 0, 1.0, 2.0, 10, 10.1, 10.2]
got = throttle.allowed(events)
assert got == want, f"allowed() = {got}, want {want}"
assert throttle.allowed([]) == []
assert throttle.allowed([5.0]) == [5.0]

src = inspect.getsource(throttle)
assert "TokenBucket" in src, "throttle.py must use ratelimit.TokenBucket, not reimplement it"
EOF
