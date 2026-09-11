#!/usr/bin/env bash
set -e
# Fresh bytecode cache: macOS system Python caches centrally (pycache_prefix),
# where a same-size same-second edit is invisible; a throwaway prefix defeats it.
export PYTHONPYCACHEPREFIX="$(mktemp -d)"
find . -name '__pycache__' -type d -prune -exec rm -rf {} +
sum="$(shasum -a 256 tests/test_versions.py 2>/dev/null | cut -d' ' -f1 || sha256sum tests/test_versions.py | cut -d' ' -f1)"
if [ "$sum" != "6c0197d9f8e33f7fbd390264dbb2101166bd183f376fecff7c8e41c57a2c2e3d" ]; then
  echo "tests/test_versions.py was modified; fix the source instead" >&2
  exit 1
fi
python3 -m unittest discover -s tests
