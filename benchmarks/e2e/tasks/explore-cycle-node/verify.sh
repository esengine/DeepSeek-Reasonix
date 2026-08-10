#!/usr/bin/env bash
set -e
got="$(tr -d '[:space:]' < answer.txt | tr '[:upper:]' '[:lower:]')"
if [ "$got" != "gorsefen" ]; then
  echo "answer.txt normalized to '$got', want 'gorsefen'" >&2
  exit 1
fi
