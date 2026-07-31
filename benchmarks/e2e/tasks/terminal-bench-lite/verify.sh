set -e
python3 - <<'PY'
lines = open("answers.txt").read().strip().splitlines()
assert len(lines) == 3, f"want 3 lines, got {lines}"
biggest, log_count, secret = lines[0].strip(), lines[1].strip(), lines[2].strip()
assert biggest == "big.txt", f"largest file: {biggest}"
assert log_count == "2", f"log count: {log_count}"
assert secret == "s3cr3t-k3y-42", f"secret: {secret}"
PY
