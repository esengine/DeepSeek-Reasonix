#!/usr/bin/env bash
set -e
# The finding is two words; establishing it is a sweep of twelve near-identical
# files. That gap between the cost of the research and the size of its result is
# the shape a dependency edge is supposed to pay for.
test -f ANSWER.md
grep -qE '^[[:space:]]*handler=.*h07(\.py)?[[:space:]]*$' ANSWER.md
grep -qE '^[[:space:]]*missing=validate_payload[[:space:]]*$' ANSWER.md
