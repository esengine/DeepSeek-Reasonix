package handlers

import (
	"testing"

	"example.com/svc/store"
)

func TestCreateOrderAtLimit(t *testing.T) {
	// Quantity sits exactly on its ceiling, which the limits are documented to
	// include. Weight and priority are well inside theirs.
	if err := CreateOrder(store.Order{ID: "o1", Quantity: 170, Weight: 10, Priority: 100}); err != nil {
		t.Fatalf("order at the inclusive limit was rejected: %v", err)
	}
}

func TestCreateOrderOverLimit(t *testing.T) {
	if err := CreateOrder(store.Order{ID: "o2", Quantity: 171, Weight: 10, Priority: 100}); err == nil {
		t.Fatal("order over the limit was accepted")
	}
}
