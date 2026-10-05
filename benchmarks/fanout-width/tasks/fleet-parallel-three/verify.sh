#!/usr/bin/env bash
set -e
# Three markers from three subsystems that share nothing. The shape is the
# point: a fan-out that runs them one at a time answers exactly as well and
# takes three times as long, which is what the wall-clock arm is asking about.
test -f ANSWER.md
grep -qE '^[[:space:]]*alpha=QORVEX-7741[[:space:]]*$' ANSWER.md
grep -qE '^[[:space:]]*beta=MIRALD-3208[[:space:]]*$' ANSWER.md
grep -qE '^[[:space:]]*gamma=TESSIK-9155[[:space:]]*$' ANSWER.md
