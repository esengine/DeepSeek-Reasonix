set -e
python3 ns.py > /tmp/nse-out.txt 2>&1 || { cat /tmp/nse-out.txt; exit 1; }
grep -q "ALL PASS" /tmp/nse-out.txt
python3 - <<'PY'
ans = open("answer.txt").read().strip()
want = str(3**12 - 2**19)
assert ans == want, f"answer.txt: {ans}, want {want}"
print("NS constants verified in Python")
PY
echo OK
