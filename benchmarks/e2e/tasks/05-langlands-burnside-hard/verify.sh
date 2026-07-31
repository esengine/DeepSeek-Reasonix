set -e
python3 langlands.py > /tmp/lang-out.txt 2>&1 || { cat /tmp/lang-out.txt; exit 1; }
grep -q "ALL PASS" /tmp/lang-out.txt
python3 - <<'PY'
ans = open("answer.txt").read().strip()
assert ans == "4", f"answer.txt: {ans}, want 4"
print("Langlands A4 class count verified: 4")
PY
echo OK
