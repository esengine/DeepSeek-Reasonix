#!/usr/bin/env bash
set -e
export PYTHONPYCACHEPREFIX="$(mktemp -d)"
python3 - <<'PY'
import inspect
import json
import tempfile

import report

stats = report.summarize([1, 2, 3])
assert stats == {"count": 3, "total": 6, "mean": 2.0, "max": 3}, stats
assert report.summarize([]) == {"count": 0, "total": 0, "mean": 0, "max": None}
assert report.summarize([1, 2, 3]) == stats, "summarize must be pure"
src = inspect.getsource(report.summarize)
assert "open(" not in src, "summarize must not touch files"

with tempfile.NamedTemporaryFile(suffix=".json", delete=False) as f:
    path = f.name
report.write_report(path, [1, 2, 3])
with open(path) as f:
    assert json.load(f) == {"count": 3, "total": 6, "mean": 2.0, "max": 3}
assert "summarize(" in inspect.getsource(report.write_report)
PY
