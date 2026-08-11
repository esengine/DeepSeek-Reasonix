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
		json.RawMessage(`null`),
		json.RawMessage(`{"type":"object"}`),
	} {
		got := string(CanonicalizeSchema(raw))
		want := `{"properties":{},"type":"object"}`
		if got != want {
			t.Fatalf("CanonicalizeSchema(%s) = %s, want %s", string(raw), got, want)
		}
	}
}

func TestCanonicalizeSchemaAddsMissingRootType(t *testing.T) {
	for _, tc := range []struct{ raw, want string }{
		{`{}`, `{"properties":{},"type":"object"}`},
		{`{"properties":{"q":{"type":"string"}}}`, `{"properties":{"q":{"type":"string"}},"type":"object"}`},
		// Explicit non-object root types are preserved verbatim; validation
		// quarantines them instead of silently rewriting declared semantics.
		{`{"type":"string"}`, `{"type":"string"}`},
		{`{"type":["object","null"]}`, `{"type":["object","null"]}`},
	} {
		if got := string(CanonicalizeSchema(json.RawMessage(tc.raw))); got != tc.want {
			t.Fatalf("CanonicalizeSchema(%s) = %s, want %s", tc.raw, got, tc.want)
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

func TestNormalizeLegacyTupleItemsForDraft202012(t *testing.T) {
	raw := json.RawMessage(`{
		"type":"object",
		"properties":{
			"pair":{
				"type":"array",
				"items":[{"type":"string"},{"type":"number"}],
				"additionalItems":false
			}
		}
	}`)

	got := string(NormalizeLegacyTupleItemsForDraft202012(raw))
	want := `{"properties":{"pair":{"items":false,"prefixItems":[{"type":"string"},{"type":"number"}],"type":"array"}},"type":"object"}`
	if got != want {
		t.Fatalf("NormalizeLegacyTupleItemsForDraft202012() = %s, want %s", got, want)
	}
	if again := string(NormalizeLegacyTupleItemsForDraft202012(json.RawMessage(got))); again != got {
		t.Fatalf("tuple migration is not idempotent:\n first: %s\nsecond: %s", got, again)
	}
}

func TestNormalizeLegacyTupleItemsForDraft202012Nested(t *testing.T) {
	raw := json.RawMessage(`{
		"type":"object",
		"$defs":{
			"value":{
				"anyOf":[
					{"type":"string"},
					{"type":"array","items":[{"type":"string"},{"type":"number"}]}
				]
			}
		},
		"properties":{"value":{"$ref":"#/$defs/value"}}
	}`)

	got := string(NormalizeLegacyTupleItemsForDraft202012(raw))
	want := `{"$defs":{"value":{"anyOf":[{"type":"string"},{"prefixItems":[{"type":"string"},{"type":"number"}],"type":"array"}]}},"properties":{"value":{"$ref":"#/$defs/value"}},"type":"object"}`
	if got != want {
		t.Fatalf("NormalizeLegacyTupleItemsForDraft202012() = %s, want %s", got, want)
	}
}

func TestNormalizeLegacyTupleItemsPreservesExistingPrefixItems(t *testing.T) {
	raw := json.RawMessage(`{
		"type":"array",
		"prefixItems":[{"type":"boolean"}],
		"items":[{"type":"string"}],
		"additionalItems":{"type":"number"}
	}`)

	got := string(NormalizeLegacyTupleItemsForDraft202012(raw))
	want := `{"items":{"type":"number"},"prefixItems":[{"type":"boolean"}],"type":"array"}`
	if got != want {
		t.Fatalf("NormalizeLegacyTupleItemsForDraft202012() = %s, want %s", got, want)
	}
}

func TestNormalizeLegacyTupleItemsPreservesModernArrayItems(t *testing.T) {
	// Modern object-form items need no rewrite, so the input bytes come back
	// exactly — not a reserialized (key-sorted) copy, which would perturb the
	// canonical bytes the registry produced once.
	raw := json.RawMessage(`{"type":"array","items":{"anyOf":[{"type":"string"},{"type":"number"}]}}`)
	if got := string(NormalizeLegacyTupleItemsForDraft202012(raw)); got != string(raw) {
		t.Fatalf("NormalizeLegacyTupleItemsForDraft202012() = %s, want the input bytes unchanged %s", got, raw)
	}
}

func TestCanonicalizeSchemaPreservesLegacyTupleItems(t *testing.T) {
	raw := json.RawMessage(`{"type":"object","properties":{"pair":{"type":"array","items":[{"type":"string"},{"type":"number"}],"additionalItems":false}}}`)
	want := `{"properties":{"pair":{"additionalItems":false,"items":[{"type":"string"},{"type":"number"}],"type":"array"}},"type":"object"}`
	if got := string(CanonicalizeSchema(raw)); got != want {
		t.Fatalf("CanonicalizeSchema() = %s, want provider-neutral bytes %s", got, want)
	}
}
