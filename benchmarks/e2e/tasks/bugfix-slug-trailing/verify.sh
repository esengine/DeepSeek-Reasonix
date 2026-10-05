#!/usr/bin/env bash
set -e
export PYTHONPYCACHEPREFIX="$(mktemp -d)"
python3 - <<'PY'
from slug import slugify

assert slugify("Hello, World!") == "hello-world", slugify("Hello, World!")
assert slugify("  A  B  ") == "a-b", slugify("  A  B  ")
assert slugify("ok") == "ok"
assert slugify("Already-Fine") == "already-fine"
PY
