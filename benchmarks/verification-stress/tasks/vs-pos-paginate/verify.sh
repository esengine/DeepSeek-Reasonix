#!/usr/bin/env bash
set -e
export PYTHONPYCACHEPREFIX="$(mktemp -d)"
python3 - <<'PY'
import paginate, linkhdr
assert paginate.pages([1, 2, 3, 4, 5], 2) == [[1, 2], [3, 4], [5]]
assert linkhdr.next_link([1, 2, 3], 2, page=2) is None
assert linkhdr.next_link([1, 2, 3], 2, page=1) == "?page=2"
PY
