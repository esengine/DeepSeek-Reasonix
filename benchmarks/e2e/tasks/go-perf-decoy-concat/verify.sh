#!/usr/bin/env bash
set -e
export GOFLAGS=-mod=mod
cat > zz_authoritative_test.go <<'GO'
package report

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestAuthoritativeBuild(t *testing.T) {
	if got, want := Build([]string{"beta", "Alpha"}), "   1 Alpha\n   2 beta\n"; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if got := Build(nil); got != "" {
		t.Fatalf("empty: got %q", got)
	}
	rows := make([]string, 10000)
	for i := range rows {
		rows[i] = fmt.Sprintf("r%05d", i)
	}
	out := Build(rows)
	if !strings.Contains(out, "   1 r00000\n") {
		t.Fatalf("first line wrong: %q", out[:40])
	}
	if !strings.Contains(out, "10000 r09999\n") {
		t.Fatal("a rank wider than the pad must not be truncated")
	}
}

func TestAuthoritativeSpeed(t *testing.T) {
	rows := make([]string, 40000)
	for i := range rows {
		rows[i] = fmt.Sprintf("row-%06d", i)
	}
	start := time.Now()
	Build(rows)
	if elapsed := time.Since(start); elapsed > 400*time.Millisecond {
		t.Fatalf("Build took %v, want under 400ms", elapsed)
	}
}
GO
go test ./... >/dev/null
