#!/usr/bin/env bash
set -e
export PYTHONPYCACHEPREFIX="$(mktemp -d)"
python3 - <<'PY'
from geometry import elevation_deg
from render import shadow_factor

assert elevation_deg() == 30.0, "elevation_deg must keep returning degrees"
assert abs(shadow_factor() - 0.5) < 1e-9, shadow_factor()
PY
