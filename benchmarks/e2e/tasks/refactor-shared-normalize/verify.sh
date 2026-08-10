#!/usr/bin/env bash
set -e
export PYTHONPYCACHEPREFIX="$(mktemp -d)"
python3 - <<'PY'
import inspect

import orgs
import textnorm
import users

assert textnorm.normalize_name("  Ada   Lovelace ") == "ada lovelace"
assert users.user_key("  Ada   Lovelace ") == "user:ada lovelace"
assert orgs.org_key(" ACME  Corp ") == "org:acme corp"
for mod in (users, orgs):
    src = inspect.getsource(mod)
    assert "def _normalize_name" not in src, f"{mod.__name__} still owns a private copy"
    assert "textnorm" in src, f"{mod.__name__} must use the shared module"
PY
