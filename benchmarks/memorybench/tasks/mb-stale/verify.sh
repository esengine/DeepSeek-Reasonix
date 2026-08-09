#!/usr/bin/env bash
set -e
grep -q "release/1.21" answer.txt && ! grep -q "release/0.9" answer.txt
