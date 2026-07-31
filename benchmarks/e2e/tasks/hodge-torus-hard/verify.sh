set -e
python3 hodge.py > /tmp/hodge-out.txt 2>&1 || { cat /tmp/hodge-out.txt; exit 1; }
grep -q "ALL PASS" /tmp/hodge-out.txt
python3 - <<'PY'
ans = open("answer.txt").read().strip()
assert ans == "0", f"answer.txt: {ans}, want 0"
print("Hodge Euler characteristic verified: 0")
PY
echo OK
