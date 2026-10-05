#!/usr/bin/env bash
set -e
grep -q "eu-central-1" answer.txt && ! grep -q "us-east-1" answer.txt
