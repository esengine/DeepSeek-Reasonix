#!/usr/bin/env bash
set -e
# Fresh bytecode cache: macOS system Python caches centrally (pycache_prefix),
# where a same-size same-second edit is invisible; a throwaway prefix defeats it.
export PYTHONPYCACHEPREFIX="$(mktemp -d)"
find . -name '__pycache__' -type d -prune -exec rm -rf {} +
python3 - <<'EOF'
import inspect

import limits
import quota
import uploader

assert limits.MAX_RETRIES == 3
assert limits.MAX_UPLOAD_BYTES == 8_388_608

assert quota.attempts_left(0) == 3
assert quota.attempts_left(2) == 1
assert quota.attempts_left(9) == 0
assert quota.upload_allowed(8_388_608)
assert not quota.upload_allowed(8_388_609)
assert not quota.upload_allowed(0)

assert uploader.plan_upload(1) == {"chunks": 1, "retries": 3, "rejected": False}
assert uploader.plan_upload(2_097_153)["chunks"] == 3
assert uploader.plan_upload(8_388_609) == {"chunks": 0, "retries": 0, "rejected": True}

for mod in (quota, uploader):
    src = inspect.getsource(mod)
    assert "limits" in src, f"{mod.__name__} must read from limits.py"
    assert "8_388_608" not in src and "8388608" not in src, f"magic size still hardcoded in {mod.__name__}"
EOF
