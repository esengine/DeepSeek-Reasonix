#!/usr/bin/env bash
set -e
export PYTHONPYCACHEPREFIX="$(mktemp -d)"
rm -f events.log
python3 rebuild.py
diff events.log - <<'WANT'
1 deploy harborlight
2 rollback harborlight
3 deploy mistgate
4 audit quaywall
WANT
grep -q "eventlog" rebuild.py || { echo "rebuild.py must use the eventlog package" >&2; exit 1; }
