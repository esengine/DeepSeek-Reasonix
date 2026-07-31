set -e
python3 ym.py > /tmp/ym-out.txt 2>&1 || { cat /tmp/ym-out.txt; exit 1; }
grep -q "ALL PASS" /tmp/ym-out.txt
python3 - <<'PY'
ans = open("answer.txt").read().strip()
assert ans == "2", f"answer.txt: {ans}, want 2 (order of central -1)"
print("YM mass-gap core verified")
PY
echo OK
