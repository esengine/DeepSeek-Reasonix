set -e
python3 - <<'PY'
# Message is after byte 20; 21 garbage bytes in our file, so test tolerant:
raw = open("data/target.txt", "rb").read()
msg = raw.decode("utf-8", errors="ignore").lstrip("\x00\x01\x02")
assert "readme.txt" in msg, msg
got = open("answer.txt").read().strip()
assert got == "1337", f"got {got}"
print("OK")
PY
