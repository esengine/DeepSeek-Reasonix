#!/usr/bin/env bash
set -e
export PYTHONPYCACHEPREFIX="$(mktemp -d)"
sum="$(shasum -a 256 tests/test_textload.py 2>/dev/null | cut -d' ' -f1 || sha256sum tests/test_textload.py | cut -d' ' -f1)"
if [ "$sum" != "aa7861f6c40e3fcb0b2f134683050318bc5fb50ddb174ce647099f52d51f9c66" ]; then
  echo "tests/test_textload.py was modified; fix the source instead" >&2
  exit 1
fi
python3 -m unittest discover -s tests
