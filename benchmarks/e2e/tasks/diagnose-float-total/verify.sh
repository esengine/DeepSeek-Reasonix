#!/usr/bin/env bash
set -e
# Fresh bytecode cache: macOS system Python caches centrally (pycache_prefix),
# where a same-size same-second edit is invisible; a throwaway prefix defeats it.
export PYTHONPYCACHEPREFIX="$(mktemp -d)"
find . -name '__pycache__' -type d -prune -exec rm -rf {} +
sum="$(shasum -a 256 tests/test_invoice.py 2>/dev/null | cut -d' ' -f1 || sha256sum tests/test_invoice.py | cut -d' ' -f1)"
if [ "$sum" != "286429ade36e56b42e2480dc9804f2f1a59ca3c988cf5cc92d34327c88e19786" ]; then
  echo "tests/test_invoice.py was modified; fix the source instead" >&2
  exit 1
fi
python3 -m unittest discover -s tests
