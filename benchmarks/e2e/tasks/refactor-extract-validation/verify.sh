#!/usr/bin/env bash
set -e
# Fresh bytecode cache: macOS system Python caches centrally (pycache_prefix),
# where a same-size same-second edit is invisible; a throwaway prefix defeats it.
export PYTHONPYCACHEPREFIX="$(mktemp -d)"
find . -name '__pycache__' -type d -prune -exec rm -rf {} +
python3 - <<'EOF'
import inspect

import handlers
import validation

for handle, verb in [
    (handlers.handle_create, "created"),
    (handlers.handle_update, "updated"),
    (handlers.handle_delete, "deleted"),
]:
    assert handle({"id": 7, "kind": "note"}) == ("ok", f"{verb} note 7")
    assert handle("nope") == ("error", "not a request")
    assert handle({"kind": "note"}) == ("error", "missing id")
    assert handle({"id": 0, "kind": "note"}) == ("error", "bad id")
    assert handle({"id": "7", "kind": "note"}) == ("error", "bad id")
    assert handle({"id": 7, "kind": "meeting"}) == ("error", "bad kind")
    assert handle({"id": 7}) == ("error", "bad kind")

assert callable(validation.validate_request), "validation.validate_request must exist"
src = inspect.getsource(handlers)
assert src.count("validate_request(") >= 3, "all three handlers must call validate_request"
assert src.count("missing id") == 0, "inline validation copies must be gone from handlers.py"
EOF
