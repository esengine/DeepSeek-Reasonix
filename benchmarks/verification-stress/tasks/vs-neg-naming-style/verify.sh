#!/usr/bin/env bash
set -e
export PYTHONPYCACHEPREFIX="$(mktemp -d)"
python3 - <<'PY'
import geo, store
assert hasattr(geo, "EARTH_RADIUS_KM") and not hasattr(geo, "earthRadiusKm")
assert hasattr(geo, "dist") and not hasattr(geo, "Dist")
assert hasattr(store, "near_by") and not hasattr(store, "NearBy")
assert store.near_by(0.0001, 0.0)
assert not store.near_by(1.0, 0.0)
PY
