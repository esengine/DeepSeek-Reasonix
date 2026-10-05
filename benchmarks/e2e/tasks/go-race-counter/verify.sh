#!/usr/bin/env bash
set -e
export GOFLAGS=-mod=mod
cat > zz_authoritative_test.go <<'GO'
package counter

import (
	"sync"
	"testing"
)

func TestAuthoritativeConcurrentAdd(t *testing.T) {
	c := New()
	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 250; j++ {
				c.Add("hit")
				c.Get("hit")
			}
		}()
	}
	wg.Wait()
	if got := c.Get("hit"); got != 10000 {
		t.Fatalf("got %d, want 10000", got)
	}
}
GO
go test -race ./... >/dev/null
