package protocol

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"strings"
	"testing"
)

var schemaHashPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

func TestSchemaHashMatchesCanonicalBytes(t *testing.T) {
	if !schemaHashPattern.MatchString(SchemaHash()) {
		t.Fatalf("SchemaHash = %q, want sha256:<64 lowercase hex>", SchemaHash())
	}
	canonical, err := CanonicalSchemaBytes()
	if err != nil {
		t.Fatal(err)
	}
	digest := sha256.Sum256(canonical)
	recomputed := "sha256:" + hex.EncodeToString(digest[:])
	if recomputed != SchemaHash() {
		t.Fatalf("committed GeneratedSchemaHash %s does not match recomputed %s (run go run ./cmd/extension-protocol-gen -root .)",
			SchemaHash(), recomputed)
	}
}

func TestCanonicalSchemaBytesAreDeterministic(t *testing.T) {
	first, err := CanonicalSchemaBytes()
	if err != nil {
		t.Fatal(err)
	}
	second, err := CanonicalSchemaBytes()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("CanonicalSchemaBytes differ across calls")
	}
	// A fresh document build must marshal to the identical bytes, proving
	// determinism is a property of the builder, not of the sync.Once cache.
	fresh, err := BuildSchemaDocument()
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(fresh)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, encoded) {
		t.Fatal("freshly built schema document does not match canonical bytes")
	}
}

func TestSchemaDocumentStructure(t *testing.T) {
	document, err := BuildSchemaDocument()
	if err != nil {
		t.Fatal(err)
	}
	if document["$schema"] != SchemaDraft202012 {
		t.Fatalf("$schema = %v", document["$schema"])
	}
	if document["$id"] != ProtocolID || document["protocol"] != ProtocolID || document["protocolID"] != ProtocolID {
		t.Fatal("schema identity fields do not carry the protocol ID")
	}
	if document["protocolMajor"] != ProtocolMajor {
		t.Fatalf("protocolMajor = %v", document["protocolMajor"])
	}

	defs, ok := document["$defs"].(map[string]any)
	if !ok || len(defs) == 0 {
		t.Fatal("$defs is missing or empty")
	}
	methods, ok := document["methods"].(map[string]any)
	if !ok {
		t.Fatal("methods is missing")
	}
	if len(methods) != 16 {
		t.Fatalf("methods has %d entries, want 16", len(methods))
	}

	for _, spec := range Registry() {
		entry, ok := methods[string(spec.Name)].(map[string]any)
		if !ok {
			t.Fatalf("methods.%s missing", spec.Name)
		}
		if entry["direction"] != string(spec.Direction) || entry["class"] != string(spec.Class) {
			t.Fatalf("methods.%s metadata = %v/%v", spec.Name, entry["direction"], entry["class"])
		}
		if entry["notification"] != spec.Notification() {
			t.Fatalf("methods.%s notification flag wrong", spec.Name)
		}
		paramsRef, ok := entry["params"].(map[string]any)
		if !ok {
			t.Fatalf("methods.%s params is not a $ref", spec.Name)
		}
		assertRefResolves(t, defs, paramsRef["$ref"], string(spec.Name)+" params")
		if spec.Notification() {
			if entry["result"] != nil {
				t.Fatalf("methods.%s notification result = %v, want null", spec.Name, entry["result"])
			}
		} else {
			resultRef, ok := entry["result"].(map[string]any)
			if !ok {
				t.Fatalf("methods.%s result is not a $ref", spec.Name)
			}
			assertRefResolves(t, defs, resultRef["$ref"], string(spec.Name)+" result")
		}
	}

	// Every $defs entry must be a closed object schema.
	for name, raw := range defs {
		object, ok := raw.(map[string]any)
		if !ok {
			t.Fatalf("$defs.%s is not an object schema", name)
		}
		if object["type"] != "object" || object["additionalProperties"] != false {
			t.Fatalf("$defs.%s = type %v additionalProperties %v, want closed object", name, object["type"], object["additionalProperties"])
		}
	}

	// Frozen enums, events, limits, and errors land in the document.
	events, ok := document["interceptEvents"].([]any)
	if !ok || len(events) != 17 {
		t.Fatalf("interceptEvents = %v", document["interceptEvents"])
	}
	eventDef := defs["EventParams"].(map[string]any)["properties"].(map[string]any)["event"].(map[string]any)
	if enum, ok := eventDef["enum"].([]any); !ok || len(enum) != 17 {
		t.Fatalf("EventParams.event enum = %v", eventDef["enum"])
	}
	limits, ok := document["limits"].(map[string]any)
	if !ok || limits["frameBytes"] != FrameBytes || limits["externalizeFieldBytes"] != ExternalizeFieldBytes ||
		limits["contentRefChunkBytes"] != ContentRefChunkBytes || limits["contentRefObjectBytes"] != ContentRefObjectBytes {
		t.Fatalf("limits = %v", document["limits"])
	}
	if errs, ok := document["errors"].([]any); !ok || len(errs) != len(frozenErrorSpecs) {
		t.Fatalf("errors = %v", document["errors"])
	}

	// Tag-derived constraints: minLength from nonempty, minimum from min=,
	// x-externalizable from the externalizable tag.
	contentRef := defs["ContentReadParams"].(map[string]any)["properties"].(map[string]any)["contentRef"].(map[string]any)
	if contentRef["minLength"] != 1 {
		t.Fatalf("contentRef schema = %v, want minLength 1", contentRef)
	}
	offset := defs["ContentReadParams"].(map[string]any)["properties"].(map[string]any)["offset"].(map[string]any)
	if offset["minimum"] != float64(0) {
		t.Fatalf("offset schema = %v, want minimum 0", offset)
	}
	payload := defs["EventParams"].(map[string]any)["properties"].(map[string]any)["payload"]
	if payloadMap, ok := payload.(map[string]any); !ok || payloadMap["x-externalizable"] != true {
		t.Fatalf("payload schema = %v, want x-externalizable annotation", payload)
	}
}

func assertRefResolves(t *testing.T, defs map[string]any, ref any, at string) {
	t.Helper()
	name, ok := ref.(string)
	if !ok || !strings.HasPrefix(name, "#/$defs/") {
		t.Fatalf("%s ref = %v, want #/$defs/<name>", at, ref)
	}
	if _, ok := defs[strings.TrimPrefix(name, "#/$defs/")]; !ok {
		t.Fatalf("%s ref %s does not resolve in $defs", at, name)
	}
}
