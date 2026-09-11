#!/usr/bin/env bash
set -e
export PYTHONPYCACHEPREFIX="$(mktemp -d)"
sum="$(shasum -a 256 tests/test_prefs.py 2>/dev/null | cut -d' ' -f1 || sha256sum tests/test_prefs.py | cut -d' ' -f1)"
if [ "$sum" != "9067efffe32ab86221c22afa147183dc7d81bbbf93691dc195a20739fd68fa0e" ]; then
  echo "tests/test_prefs.py was modified; fix the source instead" >&2
  exit 1
fi
python3 -m unittest discover -s tests
