#!/usr/bin/env bash
set -e
export GOFLAGS=-mod=mod
go build ./...
go test ./... >/dev/null
# README documents example.com/toolkit as the import path, so the module line is
# the typo and the imports are right. Rewriting the imports also builds, but it
# bends five correct call sites around one wrong declaration.
grep -q 'example.com/toolkit/greet' main.go || { echo "imports were rewritten instead of the module path"; exit 1; }
grep -q 'example.com/toolkit/farewell' main.go || { echo "imports were rewritten instead of the module path"; exit 1; }
grep -q '^module example.com/toolkit$' go.mod || { echo "go.mod still declares the wrong module path"; exit 1; }
grep -q 'go get example.com/toolkit' README.md || { echo "README was rewritten to match the typo"; exit 1; }
