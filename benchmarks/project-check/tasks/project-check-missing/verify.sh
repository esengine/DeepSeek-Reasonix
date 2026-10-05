#!/usr/bin/env bash
set -e
# The production outcome only. What the shadow probe classified is read from the
# trajectory, never asserted here: a corpus that knows the expected divergence
# class would fail together with the classifier it is supposed to check.
python3 - <<'EOF'
from calc import total
assert total([1, -2, 3]) == 4
assert total([1, 2, 3]) == 6
EOF
