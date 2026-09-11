#!/usr/bin/env bash
set -e
export PYTHONPYCACHEPREFIX="$(mktemp -d)"
python3 - <<'PY'
import tempfile
import merge
def w(text):
    f = tempfile.NamedTemporaryFile('w', suffix='.csv', delete=False)
    f.write(text); f.close(); return f.name
a = w("Name, Age\nana, 31\nbo, 22\n"); b = w("Name, Age\ncy, 45\n")
m = merge.merge_files([a, b])
assert m[0] == "name,age", m[0]
assert m[1:] == ["ana,31", "bo,22", "cy,45"], m
PY
