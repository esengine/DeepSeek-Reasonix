package main

import (
	"context"
	"encoding/json"
	"testing"
)

func TestAdvanceMovesACardOneColumnAndTheLastColumnKeepsIt(t *testing.T) {
	b := newBoard()
	if err := b.advance("c"); err != nil {
		t.Fatal(err)
	}
	if err := b.advance("a"); err != nil {
		t.Fatal(err)
	}
	if err := b.advance("a"); err == nil {
		t.Fatal("a done card moved past the last column")
	}
	raw, fallback := b.props()
	var props struct {
		Columns map[string][]card `json:"columns"`
	}
	if err := json.Unmarshal(raw, &props); err != nil {
		t.Fatal(err)
	}
	if len(props.Columns["todo"]) != 0 || len(props.Columns["doing"]) != 2 || len(props.Columns["done"]) != 1 {
		t.Fatalf("columns = %+v", props.Columns)
	}
	if fallback != "todo 0 · doing 2 · done 1" {
		t.Fatalf("fallback = %q", fallback)
	}
}

func TestAnUnknownCardOrActionIsRefused(t *testing.T) {
	b := newBoard()
	if err := b.advance("nope"); err == nil {
		t.Fatal("an unknown card moved")
	}
	if err := b.action(context.Background(), "delete", nil); err == nil {
		t.Fatal("an undeclared action ran")
	}
}
