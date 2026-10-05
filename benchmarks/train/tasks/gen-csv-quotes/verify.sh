#!/usr/bin/env bash
# Grader: the CSV reader must handle quoted fields that contain the separator.
# Runs from a directory holding the (fixed) workdir contents at the top level,
# so it references csv_reader.py and cli.py directly.
set -e

tmpdir="$(mktemp -d)"
trap 'rm -rf "$tmpdir"' EXIT

# 1. Behavioural checks against the public API (python3 stdlib only).
python3 - <<'PY'
from csv_reader import parse_csv
import sys

cases = [
    # plain data must keep working
    ("x,y", [["x", "y"]]),
    ("1,2\n3,4", [["1", "2"], ["3", "4"]]),
    ("a,,c", [["a", "", "c"]]),
    ("a,", [["a", ""]]),
    ("", []),
    # quoted fields
    ('"plain",x', [["plain", "x"]]),
    ('a,"b,c",d', [["a", "b,c", "d"]]),                  # separator inside quotes
    ('"a,b"\n"c,d"', [["a,b"], ["c,d"]]),                # separator + quotes on every row
    ('a,"b\nc",d', [["a", "b\nc", "d"]]),                # newline inside quotes
    ('"he said ""hi""",ok', [['he said "hi"', "ok"]]),   # escaped quotes
    ('a,b\n"c""d",e', [["a", "b"], ['c"d', "e"]]),       # escaped quote mid-field
    ('"",x', [["", "x"]]),                               # empty quoted field
    ("1,2\r\n3,4", [["1", "2"], ["3", "4"]]),            # CRLF line endings
]

for text, expected in cases:
    got = parse_csv(text)
    if got != expected:
        sys.stderr.write("FAIL: parse_csv(%r) = %r, expected %r\n" % (text, got, expected))
        sys.exit(1)

print("parse_csv: %d behavioural cases passed" % len(cases))
PY

# 2. End-to-end check through the CLI with a file on disk.
cat > "$tmpdir/sample.csv" <<'EOF'
name,note
"Ada, Countess of Lovelace","wrote ""notes"""
EOF

expected=$'name|note\nAda, Countess of Lovelace|wrote "notes"'
# Windows' python writes CRLF in text mode, so strip the carriage returns
# before comparing: the grader checks the fields, not the host's line endings.
got="$(python3 cli.py "$tmpdir/sample.csv" | tr -d '\r')"
if [ "$got" != "$expected" ]; then
    printf 'FAIL: cli.py printed:\n%s\n\nexpected:\n%s\n' "$got" "$expected" >&2
    exit 1
fi
echo "cli.py end-to-end check passed"

echo "ALL CHECKS PASSED"
