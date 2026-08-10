#!/usr/bin/env bash
set -e
# Fresh bytecode cache: macOS system Python caches centrally (pycache_prefix),
# where a same-size same-second edit is invisible; a throwaway prefix defeats it.
export PYTHONPYCACHEPREFIX="$(mktemp -d)"
find . -name '__pycache__' -type d -prune -exec rm -rf {} +
python3 - <<'EOF'
import api
import client

assert api.timeout_ms() == 30_000, "timeout_ms() must keep meaning milliseconds"
assert client.wait_seconds() == 30, client.wait_seconds()
assert client.retry_budget_seconds() == 90, client.retry_budget_seconds()
assert client.retry_budget_seconds(2) == 60
EOF
