#!/usr/bin/env bash
set -e
export PYTHONPYCACHEPREFIX="$(mktemp -d)"
rm -f summary.json
python3 sales_report.py sales.csv summary.json
python3 - <<'PY'
import json

with open("summary.json") as f:
    summary = json.load(f)
assert summary == {
    "east": {"total": 970.0, "top": "lanterns"},
    "north": {"total": 160.5, "top": "sails"},
    "west": {"total": 355.0, "top": "anchors"},
}, summary
assert list(summary) == sorted(summary), "keys must be sorted"
PY
# 泛化检查:换一份数据(含平手)再跑一次
cat > alt.csv <<'CSV'
region,product,units,unit_price
south,kegs,2,10.0
south,anchors,1,20.0
CSV
python3 sales_report.py alt.csv alt.json
python3 - <<'PY'
import json

with open("alt.json") as f:
    alt = json.load(f)
assert alt == {"south": {"total": 40.0, "top": "anchors"}}, alt
PY
