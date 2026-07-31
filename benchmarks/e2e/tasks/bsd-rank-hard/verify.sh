set -e
python3 bsd.py > /tmp/bsd-out.txt 2>&1 || { cat /tmp/bsd-out.txt; exit 1; }
grep -q "ALL PASS" /tmp/bsd-out.txt
python3 - <<'PY'
ans = open("answer.txt").read().strip()
assert ans == "16", f"answer.txt: {ans}, want 16"
print("BSD GF(9) point count verified: 16")
PY
echo OK
