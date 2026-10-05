#!/usr/bin/env bash
set -e
export PYTHONPYCACHEPREFIX="$(mktemp -d)"
python3 - <<'PY'
import migrate
got = migrate.upgrade({"id": 1, "fullname": "Ana Maria Silva"})
assert got == {"id": 1, "first": "Ana Maria", "last": "Silva"}, got
got = migrate.upgrade({"id": 2, "fullname": "Cher"})
assert got == {"id": 2, "first": "Cher", "last": ""}, got
import io, json, sys, contextlib
sys.stdin = io.StringIO(json.dumps({"id": 3, "fullname": "Bo Li"}) + "\n")
buf = io.StringIO()
with contextlib.redirect_stdout(buf):
    migrate.main()
assert json.loads(buf.getvalue()) == {"id": 3, "first": "Bo", "last": "Li"}
PY
