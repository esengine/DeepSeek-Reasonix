#!/usr/bin/env bash
set -e
export GOFLAGS=-mod=mod
go test ./... >/dev/null
cat > handlers/zz_authoritative_test.go <<'GO'
package handlers

import (
	"testing"

	"example.com/svc/store"
)

func TestAuthoritativeInclusiveLimit(t *testing.T) {
	if err := CreateOrder(store.Order{ID: "a", Quantity: 170, Weight: 10, Priority: 100}); err != nil {
		t.Fatalf("at the limit: %v", err)
	}
	if err := CreateOrder(store.Order{ID: "b", Quantity: 169, Weight: 10, Priority: 100}); err != nil {
		t.Fatalf("under the limit: %v", err)
	}
	if err := CreateOrder(store.Order{ID: "c", Quantity: 171, Weight: 10, Priority: 100}); err == nil {
		t.Fatal("over the limit was accepted")
	}
}
GO
go test -run TestAuthoritative ./... >/dev/null
# Exactly one validator is wrong. Rewriting all forty into the same shape also
# turns the test green, but that is coverage, not localisation.
wrong=$(grep -l ">= max" validate/*.go 2>/dev/null | wc -l | tr -d " ")
test "$wrong" = "0" || { echo "an inclusive-limit check still rejects its own ceiling"; exit 1; }
changed=$(grep -L "n > max" validate/[a-p]*.go validate/[r-z]*.go 2>/dev/null | grep -v limits.go | wc -l | tr -d " ")
test "$changed" = "0" || { echo "$changed unrelated validators were rewritten"; exit 1; }
