#!/usr/bin/env bash
set -e
export PYTHONPYCACHEPREFIX="$(mktemp -d)"
python3 - <<'PY'
import schema, serializer, consumer
assert "handle" in schema.FIELDS and "username" not in schema.FIELDS
rec = {"handle": "ana", "email": "a@x.io"}
assert serializer.load(serializer.dump(rec)) == rec
assert consumer.display_name("ana|a@x.io") == "ana"
PY
