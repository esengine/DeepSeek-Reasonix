#!/usr/bin/env bash
set -e
export GOFLAGS=-mod=mod
# Carried by the grader: loosening the threshold in index_test.go does not count.
cat > zz_authoritative_test.go <<'GO'
package index

import (
	"fmt"
	"reflect"
	"testing"
	"time"
)

func TestAuthoritativeIndexBehaviour(t *testing.T) {
	got := Index([]string{"a b a", "b c", "a"})
	want := map[string][]int{"a": {0, 2}, "b": {0, 1}, "c": {1}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if g := Index([]string{"x x x"}); !reflect.DeepEqual(g, map[string][]int{"x": {0}}) {
		t.Fatalf("same word repeated on one line: %v", g)
	}
	if g := Index(nil); len(g) != 0 {
		t.Fatalf("empty input: %v", g)
	}
}

func TestAuthoritativeIndexSpeed(t *testing.T) {
	lines := make([]string, 40000)
	for i := range lines {
		lines[i] = fmt.Sprintf("the common word number %d", i)
	}
	start := time.Now()
	got := Index(lines)
	if elapsed := time.Since(start); elapsed > 300*time.Millisecond {
		t.Fatalf("Index took %v, want under 300ms", elapsed)
	}
	if len(got["the"]) != len(lines) {
		t.Fatalf("the appears on %d lines, want %d", len(got["the"]), len(lines))
	}
}
GO
go test ./... >/dev/null
