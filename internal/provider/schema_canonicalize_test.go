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

func TestCanonicalizeSchemaDependentRequired(t *testing.T) {
	raw := json.RawMessage(`{
		"type":"object",
		"dependentRequired":{
			"cc":["billing_address","name"],
			"bad":true
		}
	}`)

	got := string(CanonicalizeSchema(raw))
	want := `{"dependentRequired":{"cc":["billing_address","name"]},"type":"object"}`
	if got != want {
		t.Fatalf("CanonicalizeSchema() = %s, want %s", got, want)
	}
}
