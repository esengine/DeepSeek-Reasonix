package provider

import (
	"encoding/json"
	"testing"
)

func TestCanonicalizeSchemaDropsNonArrayRequired(t *testing.T) {
	raw := json.RawMessage(`{
		"type":"object",
		"properties":{
			"query":{"type":"string","required":true},
			"nested":{"type":"object","required":false,"properties":{"x":{"type":"string"}}}
		},
		"required":["query","nested"]
	}`)

	got := string(CanonicalizeSchema(raw))
	want := `{"properties":{"nested":{"properties":{"x":{"type":"string"}},"type":"object"},"query":{"type":"string"}},"required":["nested","query"],"type":"object"}`
	if got != want {
		t.Fatalf("CanonicalizeSchema() = %s, want %s", got, want)
	}
}

func TestCanonicalizeSchemaAddsEmptyPropertiesForNoArgumentObject(t *testing.T) {
	for _, raw := range []json.RawMessage{
		nil,
		json.RawMessage(`{"type":"object"}`),
	} {
		got := string(CanonicalizeSchema(raw))
		want := `{"properties":{},"type":"object"}`
		if got != want {
			t.Fatalf("CanonicalizeSchema(%s) = %s, want %s", string(raw), got, want)
		}
	}
}

func TestCanonicalizeSchemaPreservesPropertyNamedRequired(t *testing.T) {
	raw := json.RawMessage(`{
		"type":"object",
		"properties":{
			"required":{"type":"boolean","description":"Whether the item is required"},
			"normal":{"type":"string"}
		},
		"required":["required"]
	}`)

	got := string(CanonicalizeSchema(raw))
	want := `{"properties":{"normal":{"type":"string"},"required":{"description":"Whether the item is required","type":"boolean"}},"required":["required"],"type":"object"}`
	if got != want {
		t.Fatalf("CanonicalizeSchema() = %s, want %s", got, want)
	}
}

func TestCanonicalizeSchemaDependentRequired(t *testing.T) {
	raw := json.RawMessage(`{
		"type":"object",
		"dependentRequired":{
			"cc":["billing_address","name"],
			"bad":true
		}
	}`)

	got := string(CanonicalizeSchema(raw))
	want := `{"dependentRequired":{"cc":["billing_address","name"]},"properties":{},"type":"object"}`
	if got != want {
		t.Fatalf("CanonicalizeSchema() = %s, want %s", got, want)
	}
}

func TestCanonicalizeSchemaPreservesDependentRequiredPropertyName(t *testing.T) {
	raw := json.RawMessage(`{
		"type":"object",
		"properties":{
			"dependentRequired":{"type":"string"},
			"x":{"type":"string"}
		},
		"dependentRequired":{
			"dependentRequired":["x"]
		}
	}`)

	got := string(CanonicalizeSchema(raw))
	want := `{"dependentRequired":{"dependentRequired":["x"]},"properties":{"dependentRequired":{"type":"string"},"x":{"type":"string"}},"type":"object"}`
	if got != want {
		t.Fatalf("CanonicalizeSchema() = %s, want %s", got, want)
	}
}
