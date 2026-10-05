#!/usr/bin/env bash
# Grader: the three public merge functions must all implement the same
# recursive deep-merge (override wins, non-dict override replaces, inputs
# never mutated). Grades public behaviour only; no knowledge of internals.
#
# Runs from a directory that contains the agent's merger.py at the top level.
set -euo pipefail

cd "$(dirname "$0")"

python3 - <<'PY'
import copy

import merger

FUNCTIONS = (merger.merge_config, merger.merge_settings, merger.merge_profile)


def reference(base, override):
    """Spec: recursive deep merge, override wins, non-dict override replaces."""
    result = dict(base)
    for key, value in override.items():
        if key in result and isinstance(result[key], dict) and isinstance(value, dict):
            result[key] = reference(result[key], value)
        else:
            result[key] = value
    return result


CASES = [
    ({}, {}),
    ({"a": 1}, {"b": 2}),
    ({"a": 1}, {"a": 2}),
    ({"a": 0}, {"a": 1}),
    ({"a": 1}, {"a": 0}),
    ({"a": None}, {"a": 1}),
    ({"a": 1}, {"a": None}),
    ({"a": {"b": 1, "c": 2}}, {"a": {"b": 3}}),
    ({"a": {"b": 1}}, {"a": 5}),
    ({"a": 5}, {"a": {"b": 1}}),
    ({"a": {}}, {"a": {"b": 1}}),
    ({"a": {"b": {"c": 1}}}, {"a": {"b": {"c": 2, "d": 3}}}),
    ({"x": {"y": {"z": [1, 2]}}}, {"x": {"y": {"z": [3]}}}),
    ({"a": {"b": 1, "c": 2}, "d": [1, 2]}, {"a": {"b": 3}, "d": [9]}),
]


def cases():
    for base, override in CASES:
        yield copy.deepcopy(base), copy.deepcopy(override)


# 1) Every public function must match the spec.
for fn in FUNCTIONS:
    for base, override in cases():
        got = fn(copy.deepcopy(base), copy.deepcopy(override))
        expected = reference(copy.deepcopy(base), copy.deepcopy(override))
        if got != expected:
            raise AssertionError(
                "%s(%r, %r) returned %r, expected %r"
                % (fn.__name__, base, override, got, expected)
            )

# 2) The three call sites must agree with each other on every case.
for base, override in cases():
    first = FUNCTIONS[0](copy.deepcopy(base), copy.deepcopy(override))
    for fn in FUNCTIONS[1:]:
        other = fn(copy.deepcopy(base), copy.deepcopy(override))
        if other != first:
            raise AssertionError(
                "%s disagrees with %s on %r <- %r: %r vs %r"
                % (fn.__name__, FUNCTIONS[0].__name__, base, override, other, first)
            )

# 3) Neither input may be mutated.
for fn in FUNCTIONS:
    for base, override in cases():
        before_b, before_o = copy.deepcopy(base), copy.deepcopy(override)
        fn(base, override)
        if base != before_b or override != before_o:
            raise AssertionError("%s mutated its inputs" % fn.__name__)

print("OK: all three functions match the deep-merge spec, agree with each other, and never mutate inputs.")
PY
