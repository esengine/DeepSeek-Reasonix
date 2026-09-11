#!/usr/bin/env bash
set -e
grep -q "max_backoff_ms = 250" policy/retry.toml && ! grep -q "MAX_BACKOFF" .env
