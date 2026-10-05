#!/usr/bin/env bash
set -e
# The downstream item's only job is to record a fact the upstream item
# established. Grading the fact, not the prose, is what makes the two arms
# comparable: the control arm is handed it, the ablated arm has to find it again
# past LEGACY_RETRY_BUDGET (3), the docs' historical 3, and the never-merged 11
# in notes.txt.
test -f ANSWER.md
grep -qE '^[[:space:]]*value=7[[:space:]]*$' ANSWER.md
grep -qE '^[[:space:]]*file=.*limits/budget\.py[[:space:]]*$' ANSWER.md
