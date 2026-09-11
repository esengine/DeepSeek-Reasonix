#!/usr/bin/env bash
set -e
grep -q "https://api.v2.example" answer.txt && ! grep -q "api.v1.example" answer.txt
