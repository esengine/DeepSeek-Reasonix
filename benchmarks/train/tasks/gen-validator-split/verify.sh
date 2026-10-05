#!/usr/bin/env bash
# Grader for the record-loader refactor task.
# Runs from a directory that contains records.py at the top level.
set -e

python3 - <<'PY'
import os
import sys
import tempfile

sys.path.insert(0, os.getcwd())
import records

# --- Requirement: validation is separated from I/O ------------------------
# validate_record must be a public, callable, standalone function.
assert callable(getattr(records, "validate_record", None)), \
    "records.validate_record is missing or not callable"

# Valid lines return the expected tuple, including surrounding whitespace.
assert records.validate_record("Ada,36,ada@example.com") == ("Ada", 36, "ada@example.com")
assert records.validate_record("Grace,45,grace@navy.mil") == ("Grace", 45, "grace@navy.mil")
assert records.validate_record("  Bob  ,  22 ,  bob@example.org ") == ("Bob", 22, "bob@example.org")
assert records.validate_record("Eve,0,eve@example.org") == ("Eve", 0, "eve@example.org")
assert records.validate_record("Zed,150,zed@example.net") == ("Zed", 150, "zed@example.net")

# Malformed lines raise ValueError.
for bad in (
    "a,b",                  # too few fields
    "a,b,c,d",              # too many fields
    ",5,x@y.z",             # empty name
    "a,not-an-int,x@y.z",   # non-integer age
    "a,-1,x@y.z",           # negative age
    "a,200,x@y.z",          # age out of range
    "a,5,no-at-sign",       # invalid email
    "",                     # blank line is not a valid record
):
    try:
        records.validate_record(bad)
    except ValueError:
        pass
    else:
        raise AssertionError("validate_record(%r) should raise ValueError" % bad)

# validate_record performs no file I/O: it must work in an empty directory.
with tempfile.TemporaryDirectory() as d:
    old_cwd = os.getcwd()
    os.chdir(d)
    try:
        assert records.validate_record("Zed,33,zed@example.net") == ("Zed", 33, "zed@example.net")
    finally:
        os.chdir(old_cwd)

# --- Requirement: load_records keeps its public behaviour ------------------
with tempfile.TemporaryDirectory() as d:
    path = os.path.join(d, "people.csv")
    with open(path, "w", encoding="utf-8") as f:
        f.write("Ada,36,ada@example.com\n\nGrace,45,grace@navy.mil\n")
    assert records.load_records(path) == [
        ("Ada", 36, "ada@example.com"),
        ("Grace", 45, "grace@navy.mil"),
    ]

    with open(path, "w", encoding="utf-8") as f:
        f.write("Ada,36,ada@example.com\nBogus\n")
    try:
        records.load_records(path)
    except ValueError:
        pass
    else:
        raise AssertionError("load_records should raise ValueError on an invalid record")

print("OK")
PY
