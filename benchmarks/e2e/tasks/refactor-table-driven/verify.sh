#!/usr/bin/env bash
set -e
export PYTHONPYCACHEPREFIX="$(mktemp -d)"
python3 - <<'PY'
import inspect

import statuscodes

assert statuscodes.label_for("ok") == "Completed"
assert statuscodes.label_for("wip") == "In progress"
assert statuscodes.label_for("hold") == "On hold"
assert statuscodes.label_for("???") == "Unknown"
assert statuscodes.summary(["ok", "nope", "hold"]) == "Completed, Unknown, On hold"
assert statuscodes.summary([]) == ""

assert isinstance(statuscodes.LABELS, dict), "LABELS mapping must exist"
src = inspect.getsource(statuscodes)
assert src.count("Completed") == 1, "the label text must appear exactly once"
assert src.count("In progress") == 1
PY
