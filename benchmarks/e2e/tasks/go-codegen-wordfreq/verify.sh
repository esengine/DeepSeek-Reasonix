#!/usr/bin/env bash
set -e
export GOFLAGS=-mod=mod
go build -o ./wordfreq_bin .
out=$(./wordfreq_bin -n 3 sample.txt)
[ "$(echo "$out" | sed -n 1p)" = "the 5" ] || { echo "line 1: $out"; exit 1; }
[ "$(echo "$out" | sed -n 2p)" = "fox 3" ] || { echo "line 2: $out"; exit 1; }
[ "$(echo "$out" | sed -n 3p)" = "dog 2" ] || { echo "line 3: $out"; exit 1; }
js=$(./wordfreq_bin -n 2 -json sample.txt)
python3 - "$js" <<'PY'
import json,sys
d=json.loads(sys.argv[1])
assert isinstance(d,list) and len(d)==2, d
assert d[0]=={"word":"the","count":5}, d[0]
assert d[1]=={"word":"fox","count":3}, d[1]
PY
go test ./... >/dev/null
ls *_test.go >/dev/null 2>&1 || { echo "the tool ships no tests of its own"; exit 1; }
