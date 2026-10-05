#!/usr/bin/env bash
# Grader for the semver comparator task.
# Runs from a directory holding workdir/'s contents at the top level.
set -e

cd "$(dirname "$0")"

python3 - <<'PY'
import functools
import os
import sys

sys.path.insert(0, os.getcwd())

import semver_cmp


def check(actual, expected, label):
    if actual != expected:
        raise AssertionError(
            "%s: expected %d, got %d" % (label, expected, actual)
        )


c = semver_cmp.compare

# Core version precedence
check(c("1.0.0", "1.0.0"), 0, "equal cores")
check(c("1.0.0", "2.0.0"), -1, "major")
check(c("2.0.0", "1.9.9"), 1, "major reverse")
check(c("1.2.3", "1.2.4"), -1, "patch")
check(c("10.2.3", "9.9.9"), 1, "multi-digit core")

# Pre-release vs. release
check(c("1.0.0-alpha", "1.0.0"), -1, "pre < release")
check(c("1.0.0", "1.0.0-alpha"), 1, "release > pre")

# Pre-release identifier ordering
check(c("1.0.0-alpha", "1.0.0-alpha"), 0, "same pre-release")
check(c("1.0.0-alpha", "1.0.0-beta"), -1, "alpha < beta")
check(c("1.0.0-alpha.beta", "1.0.0-beta"), -1, "alpha.beta < beta")
check(c("1.0.0-alpha.1", "1.0.0-alpha.beta"), -1, "numeric < alphanumeric")
check(c("1.0.0-alpha.beta", "1.0.0-alpha.1"), 1, "alphanumeric > numeric")
check(c("1.0.0-alpha.1", "1.0.0-alpha.1.1"), -1, "fewer fields < more fields")
check(c("1.0.0-alpha.1.1", "1.0.0-alpha.1"), 1, "more fields > fewer fields")
check(c("1.0.0-beta.11", "1.0.0-beta.2"), 1, "numeric fields compare as numbers")
check(c("1.0.0-beta.2", "1.0.0-beta.11"), -1, "numeric fields reverse")

# End-to-end sort of a mixed list
versions = [
    "1.0.0",
    "1.0.0-rc.1",
    "1.0.0-beta.11",
    "1.0.0-beta.2",
    "1.0.0-alpha",
    "0.9.9",
    "1.0.1-alpha",
]
expected = [
    "0.9.9",
    "1.0.0-alpha",
    "1.0.0-beta.2",
    "1.0.0-beta.11",
    "1.0.0-rc.1",
    "1.0.0",
    "1.0.1-alpha",
]
got = sorted(versions, key=functools.cmp_to_key(c))
if got != expected:
    raise AssertionError("sort order wrong: %r" % (got,))

print("all checks passed")
PY
