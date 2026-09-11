#!/usr/bin/env bash
set -e
export PYTHONPYCACHEPREFIX="$(mktemp -d)"
python3 - <<'PY'
import core, cache, cli
assert not hasattr(core, "fetch_all")
assert core.list_records() == {"a": 1, "b": 2}
assert cache.list_records_cached() == {"a": 1, "b": 2}
import io, contextlib
buf = io.StringIO()
with contextlib.redirect_stdout(buf):
    cli.main()
assert buf.getvalue() == "a=1\nb=2\n"
PY
