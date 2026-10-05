#!/usr/bin/env bash
set -e
export PYTHONPYCACHEPREFIX="$(mktemp -d)"
python3 - <<'PY'
import tempfile

from confreader import load
from confwriter import dump

settings = {"host": "example.test", "port": "8443", "note": "a=b ok"}
with tempfile.NamedTemporaryFile(suffix=".conf", delete=False) as f:
    path = f.name
dump(settings, path)
assert load(path) == settings, load(path)
PY
