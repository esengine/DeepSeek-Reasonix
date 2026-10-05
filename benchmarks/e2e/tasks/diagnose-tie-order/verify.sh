#!/usr/bin/env bash
set -e
export PYTHONPYCACHEPREFIX="$(mktemp -d)"
sum="$(shasum -a 256 tests/test_leaderboard.py 2>/dev/null | cut -d' ' -f1 || sha256sum tests/test_leaderboard.py | cut -d' ' -f1)"
if [ "$sum" != "06d377e4e1d872afc4b7e51489b868e103b55e105f34efc4dcaa48d09656a59f" ]; then
  echo "tests/test_leaderboard.py was modified; fix the source instead" >&2
  exit 1
fi
python3 -m unittest discover -s tests
