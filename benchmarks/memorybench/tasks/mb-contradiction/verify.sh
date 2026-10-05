#!/usr/bin/env bash
set -e
grep -q "pnpm install" answer.txt && ! grep -qE '(^|[^p])npm install' answer.txt
