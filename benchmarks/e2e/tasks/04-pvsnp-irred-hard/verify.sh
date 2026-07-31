set -e
python3 pvsnp.py > /tmp/pvsnp-out.txt 2>&1 || { cat /tmp/pvsnp-out.txt; exit 1; }
grep -q "ALL PASS" /tmp/pvsnp-out.txt
python3 - <<'PY'
ans = open("answer.txt").read().strip()
assert ans == "3", f"answer.txt: {ans}, want 3"
print("PvsNP irreducible count verified: 3")
PY
echo OK
