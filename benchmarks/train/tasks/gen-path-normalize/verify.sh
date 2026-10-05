#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")"

python3 - <<'PY'
import sys

try:
    import pathutil
except Exception as exc:  # missing file, syntax error, import-time crash
    print(f"FAIL: cannot import pathutil: {exc}", file=sys.stderr)
    sys.exit(1)

normalize = getattr(pathutil, "normalize", None)
if not callable(normalize):
    print("FAIL: pathutil has no callable normalize()", file=sys.stderr)
    sys.exit(1)

CASES = [
    # identity / ordinary components
    ("a/b/c", "a/b/c"),
    ("a b/c d", "a b/c d"),
    ("a.b/c", "a.b/c"),
    ("a/..b", "a/..b"),
    # redundant separators
    ("a//b", "a/b"),
    ("a///b///c", "a/b/c"),
    ("a//", "a"),
    ("a/b//c//d", "a/b/c/d"),
    ("//a", "/a"),
    ("//", "/"),
    ("///", "/"),
    ("/", "/"),
    # dot components
    ("./a", "a"),
    ("a/.", "a"),
    ("a/./b", "a/b"),
    ("a/././b", "a/b"),
    (".", "."),
    ("./", "."),
    # dot-dot components
    ("a/../b", "b"),
    ("a/b/..", "a"),
    ("a/..", "."),
    ("a/b/../", "a"),
    ("..", ".."),
    ("../..", "../.."),
    ("../../a/..", "../.."),
    ("a/../../b", "../b"),
    ("a/../..", ".."),
    ("a/b/c/../../d", "a/d"),
    ("a/b/../../..", ".."),
    ("../a/..", ".."),
    ("/../..", "/"),
    ("a/./../b/./", "b"),
    ("a/.../..", "a"),
    # absolute paths
    ("/a/b", "/a/b"),
    ("/a/../b", "/b"),
    ("/..", "/"),
    ("/a/../../b", "/b"),
    ("/./a", "/a"),
    ("/a/.", "/a"),
    ("/a/b/..", "/a"),
    ("/a/", "/a"),
    ("/a//b/./c/", "/a/b/c"),
    ("/a/b/./", "/a/b"),
    # empty / trailing slash
    ("", "."),
    ("a/b/", "a/b"),
    ("a/../", "."),
]

failed = 0
for inp, expected in CASES:
    try:
        got = normalize(inp)
    except Exception as exc:
        print(f"FAIL: normalize({inp!r}) raised {exc!r}")
        failed += 1
        continue
    if got != expected:
        print(f"FAIL: normalize({inp!r}) = {got!r}, expected {expected!r}")
        failed += 1

if failed:
    print(f"{failed} of {len(CASES)} cases failed", file=sys.stderr)
    sys.exit(1)

print(f"OK: {len(CASES)}/{len(CASES)} cases passed")
PY
