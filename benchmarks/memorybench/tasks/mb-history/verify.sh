#!/usr/bin/env bash
set -e
grep -qF "timeout_seconds = 47   # MEMKEY-HIST6M tuned for p99" answer.txt
