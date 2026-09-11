#!/usr/bin/env bash
set -e
export PYTHONPYCACHEPREFIX="$(mktemp -d)"
sum="$(shasum -a 256 tests/test_buckets.py 2>/dev/null | cut -d' ' -f1 || sha256sum tests/test_buckets.py | cut -d' ' -f1)"
if [ "$sum" != "2ff02f1890f940ace42b4e9710dda463015ab43f45958f3f23117530b6ab90c7" ]; then
  echo "tests/test_buckets.py was modified; fix the source instead" >&2
  exit 1
fi
python3 -m unittest discover -s tests
