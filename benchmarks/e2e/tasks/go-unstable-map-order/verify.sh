#!/usr/bin/env bash
set -e
export GOFLAGS=-mod=mod
# One run proves nothing: map order is only sometimes wrong.
cat > zz_authoritative_test.go <<'GO'
package summary

import "testing"

func TestAuthoritativeStableOrder(t *testing.T) {
	in := []string{"pear", "apple", "pear", "fig", "apple", "apple"}
	first := Summary(in)
	for i := 0; i < 200; i++ {
		if got := Summary(in); got != first {
			t.Fatalf("unstable: %q vs %q", got, first)
		}
	}
	if first != "apple=3\nfig=1\npear=2\n" {
		t.Fatalf("got %q, want counts in a defined order", first)
	}
}
GO
go test -count=1 ./... >/dev/null
for i in 1 2 3 4 5; do go test -count=1 -run TestAuthoritativeStableOrder ./... >/dev/null; done
