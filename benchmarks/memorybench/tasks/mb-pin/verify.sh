#!/usr/bin/env bash
set -e
grep -q "hello" notes.txt && head -1 notes.txt | grep -q "MEMKEY-PIN4D"
