package protocolgen

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/remote/protocol"
)

func TestGeneratedArtifactsAreDeterministicAndCommitted(t *testing.T) {
	first, err := Generate()
	if err != nil {
		t.Fatal(err)
	}
	second, err := Generate()
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 3 || len(second) != len(first) {
		t.Fatalf("generated %d/%d artifacts, want 3", len(first), len(second))
	}

	temporaryRoot := t.TempDir()
	if err := Write(temporaryRoot, first); err != nil {
		t.Fatal(err)
	}
	repositoryRoot := filepath.Clean(filepath.Join("..", "..", ".."))
	for i, artifact := range first {
		if artifact.Path != second[i].Path || !bytes.Equal(artifact.Data, second[i].Data) {
			t.Fatalf("artifact %s is not deterministic", artifact.Path)
		}
		generated, err := os.ReadFile(filepath.Join(temporaryRoot, filepath.FromSlash(artifact.Path)))
		if err != nil {
			t.Fatal(err)
		}
		committed, err := os.ReadFile(filepath.Join(repositoryRoot, filepath.FromSlash(artifact.Path)))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(generated, committed) {
			t.Fatalf("committed artifact drift: %s (run make remote-protocol-generate)", artifact.Path)
		}
	}
}

func TestHydratedTypeRemovesOnlyExternalizableNull(t *testing.T) {
	schema := protocol.SchemaType{Type: "object", Properties: []protocol.SchemaProperty{
		{Name: "external", Required: true, Externalizable: true, Schema: protocol.SchemaType{Type: "string", Nullable: true}},
		{Name: "ordinary", Required: true, Schema: protocol.SchemaType{Type: "string", Nullable: true}},
	}}
	renderer := tsRenderer{}
	raw, err := renderer.render(schema, rawMode, false, 0)
	if err != nil {
		t.Fatal(err)
	}
	hydrated, err := renderer.render(schema, hydratedMode, false, 0)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(raw, " | null") != 2 {
		t.Fatalf("raw view = %s, want both nullable fields", raw)
	}
	if strings.Contains(hydrated, "\"external\": string | null") || !strings.Contains(hydrated, "\"ordinary\": string | null") {
		t.Fatalf("hydrated view = %s, want only ordinary field nullable", hydrated)
	}
}

func TestTypeScriptDiscriminatorComesFromSchemaMetadata(t *testing.T) {
	schema := protocol.SchemaType{
		Type: "object",
		Properties: []protocol.SchemaProperty{
			{Name: "enabled", Required: true, Schema: protocol.SchemaType{Type: "boolean"}},
			{Name: "kind", Required: true, Schema: protocol.SchemaType{Type: "string", Enum: []string{"a", "b"}}},
			{Name: "text", Schema: protocol.SchemaType{Type: "string"}},
		},
		Validation: &protocol.SchemaValidation{Discriminator: &protocol.SchemaDiscriminator{
			Property: "kind",
			Variants: []protocol.SchemaVariant{
				{Values: []string{"a"}, Required: []string{"text"}, RequiredTrue: []string{"enabled"}},
				{Values: []string{"b"}, Forbidden: []string{"text"}, RequiredFalse: []string{"enabled"}},
			},
		}},
	}
	generated, err := (tsRenderer{}).render(schema, hydratedMode, false, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, fragment := range []string{
		`"kind": "a"`, `"text": string`, `"enabled": true`,
		`"kind": "b"`, `"text"?: never`, `"enabled": false`,
	} {
		if !strings.Contains(generated, fragment) {
			t.Fatalf("generated union does not contain %q:\n%s", fragment, generated)
		}
	}
}

func TestCheckRejectsAnyArtifactDrift(t *testing.T) {
	artifacts, err := Generate()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := Write(root, artifacts); err != nil {
		t.Fatal(err)
	}
	if err := Check(root, artifacts); err != nil {
		t.Fatalf("freshly generated artifacts failed check: %v", err)
	}
	drifted := filepath.Join(root, filepath.FromSlash(artifacts[1].Path))
	if err := os.WriteFile(drifted, append(append([]byte(nil), artifacts[1].Data...), '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Check(root, artifacts); err == nil {
		t.Fatal("Check accepted a drifted generated artifact")
	}
}
