#!/usr/bin/env bash
set -e
export PYTHONPYCACHEPREFIX="$(mktemp -d)"
python3 - <<'PY'
from auth import bearer_token
from headers import Headers

h = Headers()
h.set("Authorization", "Bearer sekrit")
assert bearer_token(h) == "sekrit", bearer_token(h)
h2 = Headers()
h2.set("authorization", "Bearer low")
assert bearer_token(h2) == "low"
assert h2.get("AUTHORIZATION") == "Bearer low"
h3 = Headers()
assert bearer_token(h3) is None
PY
