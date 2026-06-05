package tool

import (
	"context"
	"encoding/json"
	"testing"
)

type countingSchemaTool struct {
	name  string
	calls *int
}

func (c countingSchemaTool) Name() string        { return c.name }
func (c countingSchemaTool) Description() string { return c.name }
func (c countingSchemaTool) Schema() json.RawMessage {
	*c.calls++
	return json.RawMessage(`{"type":"object","properties":{"b":{"type":"string"},"a":{"type":"string"}}}`)
}
func (c countingSchemaTool) Execute(context.Context, json.RawMessage) (string, error) {
	return "", nil
}
func (c countingSchemaTool) ReadOnly() bool { return true }

// TestSchemasCanonicalizesOncePerTool guards the regression where Schemas() — run
// every turn — re-canonicalized (unmarshal+sort+marshal) every tool's schema on
// each call. Schemas never change after registration, so Schema() must be invoked
// exactly once (at Add), no matter how many times Schemas() is called.
func TestSchemasCanonicalizesOncePerTool(t *testing.T) {
	calls := 0
	r := NewRegistry()
	r.Add(countingSchemaTool{name: "alpha", calls: &calls})

	if calls != 1 {
		t.Fatalf("Schema() called %d times at Add, want 1", calls)
	}

	for i := 0; i < 50; i++ {
		schemas := r.Schemas()
		if len(schemas) != 1 {
			t.Fatalf("Schemas() returned %d entries, want 1", len(schemas))
		}
		// Canonicalization sorts object keys, so "a" must precede "b".
		got := string(schemas[0].Parameters)
		if ai, bi := indexOf(got, `"a"`), indexOf(got, `"b"`); ai < 0 || bi < 0 || ai > bi {
			t.Fatalf("schema not canonicalized (keys unsorted): %s", got)
		}
	}

	if calls != 1 {
		t.Fatalf("Schema() called %d times after 50 Schemas() calls, want 1 (caching regressed)", calls)
	}
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
