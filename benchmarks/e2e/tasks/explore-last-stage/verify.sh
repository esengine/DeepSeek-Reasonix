#!/usr/bin/env bash
set -e
got="$(tr -d '[:space:]' < answer.txt | tr '[:upper:]' '[:lower:]')"
if [ "$got" != "orrick" ]; then
  echo "answer.txt normalized to '$got', want 'orrick'" >&2
  exit 1
fi
