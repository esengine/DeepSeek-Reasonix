package main

import (
	"context"
	"encoding/json"
	"testing"
)

func TestInterceptInputRewritesStarterPrefix(t *testing.T) {
	result, err := interceptInput(context.Background(), "input.receive", json.RawMessage(`{"text":"starter: explain sidecars"}`))
	if err != nil {
		t.Fatal(err)
	}
	if result == nil || result.Decision != "replace" {
		t.Fatalf("result = %#v, want replace", result)
	}
	want := `{"text":"explain sidecars [rewritten by starter-extension]"}`
	if string(result.Replacement) != want {
		t.Fatalf("replacement = %s, want %s", result.Replacement, want)
	}
}

func TestInterceptInputContinuesOrdinaryText(t *testing.T) {
	result, err := interceptInput(context.Background(), "input.receive", json.RawMessage(`{"text":"ordinary input"}`))
	if err != nil {
		t.Fatal(err)
	}
	if result == nil || result.Decision != "continue" {
		t.Fatalf("result = %#v, want continue", result)
	}
}
