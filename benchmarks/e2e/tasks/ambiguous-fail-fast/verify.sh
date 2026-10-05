#!/usr/bin/env bash
set -e
export PYTHONPYCACHEPREFIX="$(mktemp -d)"
python3 - <<'PY'
import json
import tempfile

from config import load


def write(cfg):
    f = tempfile.NamedTemporaryFile(mode="w", suffix=".json", delete=False)
    json.dump(cfg, f)
    f.close()
    return f.name


good = load(write({"host": "example.test", "port": 8443}))
assert good["host"] == "example.test" and good["port"] == 8443

for bad in ({"host": "example.test"}, {"host": "example.test", "port": "8443"}):
    try:
        load(write(bad))
    except Exception as err:
        assert "port" in str(err), f"error must name the offending key: {err!r}"
    else:
        raise AssertionError(f"broken config accepted: {bad}")
PY
