#!/usr/bin/env bash
set -e
export PYTHONPYCACHEPREFIX="$(mktemp -d)"
python3 - <<'PY'
import quota, tiers
assert quota.overage_units(80, "basic") == 0
assert quota.overage_units(130, "basic") == 30
assert tiers.tier_for(100) == "basic"
PY
