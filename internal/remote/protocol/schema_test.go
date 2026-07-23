package protocol

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"

	"reasonix/internal/eventwire"
)

func TestCanonicalSchemaDeterministicAndHashExact(t *testing.T) {
	first, err := CanonicalSchemaBytes()
	if err != nil {
		t.Fatal(err)
	}
	second, err := CanonicalSchemaBytes()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) || !json.Valid(first) {
		t.Fatal("canonical schema is not deterministic valid JSON")
	}
	first[0] ^= 1
	third, _ := CanonicalSchemaBytes()
	if bytes.Equal(first, third) {
		t.Fatal("CanonicalSchemaBytes returned mutable shared storage")
	}
	digest := sha256.Sum256(third)
	wantHash := "sha256:" + hex.EncodeToString(digest[:])
	if SchemaHash() != wantHash {
		t.Fatalf("SchemaHash = %s, want %s", SchemaHash(), wantHash)
	}
	text := string(third)
	for _, forbidden := range []string{"InitializeParams", "/home/taibai/DeepSeek-Reasonix", "generatedAt"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("schema contains non-wire input %q", forbidden)
		}
	}
}

func TestSchemaContainsRegistryErrorsEventsAndFrozenLimits(t *testing.T) {
	document, err := BuildSchemaDocument()
	if err != nil {
		t.Fatal(err)
	}
	if len(document.Methods) != 78 || len(document.Errors) != 51 {
		t.Fatalf("schema contains %d methods/%d errors", len(document.Methods), len(document.Errors))
	}
	for i := 1; i < len(document.Methods); i++ {
		if document.Methods[i-1].Name >= document.Methods[i].Name {
			t.Fatal("schema methods are not sorted by wire name")
		}
	}
	wantKinds := eventwire.KindNames()
	sort.Strings(wantKinds)
	if !reflect.DeepEqual(document.Event.Kinds, wantKinds) {
		t.Fatalf("event kinds drift: got %v want %v", document.Event.Kinds, wantKinds)
	}
	if document.Resources.Protocol != FrozenProtocolLimits() || document.Resources.Lease.TTLMillis != LeaseTTLMillis || document.Resources.Idempotency.PerHostEntries != IdempotencyHostEntries {
		t.Fatalf("resource contract drift: %+v", document.Resources)
	}
	if len(document.Event.ExternalizableJSONPointers) == 0 || !contains(document.Event.ExternalizableJSONPointers, "/tool/output") {
		t.Fatal("event externalization contract missing tool output")
	}
	reasonixCode := findProperty(t, document.ErrorData, "reasonixCode")
	if len(reasonixCode.Schema.Enum) != 51 {
		t.Fatalf("ReasonixErrorCode enum = %d", len(reasonixCode.Schema.Enum))
	}
	submit := findSchemaMethod(t, document, MethodSessionSubmit)
	if !findProperty(t, submit.Params, "requestId").Required || !findProperty(t, submit.Result, "kind").Required {
		t.Fatal("submit schema lost mutation envelope or result discriminator")
	}
}

type validatorWithoutSchemaContract struct {
	Value string `json:"value"`
}

func (validatorWithoutSchemaContract) Validate() error { return nil }

func TestSchemaRequiresContractsForCustomValidators(t *testing.T) {
	if _, err := buildSchemaType(reflect.TypeOf(validatorWithoutSchemaContract{})); err == nil {
		t.Fatal("custom validator without schema contract was omitted from schemaHash")
	}

	highRisk := []reflect.Type{
		typeOf[SessionSubmitResult](), typeOf[PendingPrompt](), typeOf[TopicSelection](),
		typeOf[GitCommitDetailResult](), typeOf[FilePreviewResult](), typeOf[SessionResyncRequired](),
		typeOf[CatalogChanged](), typeOf[ExternalizedField](), typeOf[HistoryPage](),
		typeOf[SessionContentResult](), typeOf[Capabilities](),
	}
	for _, typ := range highRisk {
		schema, err := buildSchemaType(typ)
		if err != nil {
			t.Fatalf("%v: %v", typ, err)
		}
		if schema.Validation == nil || (len(schema.Validation.Invariants) == 0 && schema.Validation.Discriminator == nil) {
			t.Fatalf("%v lost custom validation schema", typ)
		}
	}
	submit, err := buildSchemaType(typeOf[SessionSubmitResult]())
	if err != nil {
		t.Fatal(err)
	}
	if submit.Validation.Discriminator == nil || submit.Validation.Discriminator.Property != "kind" || len(submit.Validation.Discriminator.Variants) != 3 {
		t.Fatalf("submit discriminator schema = %+v", submit.Validation)
	}
	wantSnapshotInvariant := "snapshotRequired:false_for(kind=turn|operation|effect=none),true_for(effect=runtime_replaced|session_replaced),optional_for(effect=state_changed)"
	if !contains(submit.Validation.Invariants, wantSnapshotInvariant) {
		t.Fatalf("submit snapshot contract = %+v", submit.Validation.Invariants)
	}
}

func findSchemaMethod(t *testing.T, document SchemaDocument, method Method) SchemaMethod {
	t.Helper()
	for _, candidate := range document.Methods {
		if candidate.Name == method {
			return candidate
		}
	}
	t.Fatalf("schema method %s not found", method)
	return SchemaMethod{}
}

func findProperty(t *testing.T, schema SchemaType, name string) SchemaProperty {
	t.Helper()
	for _, property := range schema.Properties {
		if property.Name == name {
			return property
		}
	}
	t.Fatalf("property %s not found", name)
	return SchemaProperty{}
}
