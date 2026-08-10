#!/usr/bin/env bash
set -e
grep -q "pnpm install" answer.txt && ! grep -q "npm install" answer.txt
