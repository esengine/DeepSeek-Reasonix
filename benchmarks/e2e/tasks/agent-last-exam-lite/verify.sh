set -e
python3 - <<'PY'
# Riddle answer is "map" → the file is part_map.txt → number is 42
num = 42
answer = num * 2 + 1  # 85
got = int(open("answer.txt").read().strip())
assert got == answer, f"got {got}, want {answer}"
print(f"OK: {got}")
PY
