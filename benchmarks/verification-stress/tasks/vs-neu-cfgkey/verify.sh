#!/usr/bin/env bash
set -e
export PYTHONPYCACHEPREFIX="$(mktemp -d)"
python3 - <<'PY'
import json
import loader, writer
cfg = loader.load("")
assert cfg == {"host": "localhost", "port": 8080}
cfg = loader.load('{"port": 9000}')
assert cfg == {"host": "localhost", "port": 9000}
assert json.loads(writer.dump(cfg)) == cfg
assert loader.load(writer.dump(cfg)) == cfg
PY
