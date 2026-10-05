package plugin

import (
	"context"
	"encoding/json"
	"testing"
)

type instructionsTransport struct{ raw json.RawMessage }

func (t *instructionsTransport) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	if method == "initialize" {
		return t.raw, nil
	}
	return json.RawMessage(`{}`), nil
}

func (t *instructionsTransport) notify(ctx context.Context, method string, params any) error {
	return nil
}
func (t *instructionsTransport) close() {}

// A server's own account of itself arrives once, in the handshake, and nothing
// else in the protocol answers "what is this for". Dropping it left every
// status surface with a name and no way to say what the name means.
func TestInitializeKeepsWhatTheServerSaidAboutItself(t *testing.T) {
	tr := &instructionsTransport{raw: json.RawMessage(
		`{"capabilities":{"tools":{}},"instructions":"  Reads and writes Figma files.\n"}`)}
	c := &Client{name: "figma", t: tr, spec: Spec{Name: "figma"}, transport: "http"}

	if err := c.initialize(context.Background()); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	if c.instructions != "Reads and writes Figma files." {
		t.Fatalf("instructions = %q, want the trimmed server text", c.instructions)
	}

	h := NewHost()
	h.clients = append(h.clients, c)
	got := h.Servers()
	if len(got) != 1 || got[0].Description != "Reads and writes Figma files." {
		t.Fatalf("Servers() = %+v, want the description carried through", got)
	}
	if h.serverInstructions("figma") != c.instructions {
		t.Fatalf("serverInstructions = %q, want the same text the cache writer persists", h.serverInstructions("figma"))
	}
}

// A server that says nothing about itself must not acquire a description on the
// way through: a surface that invents one is worse than one that says nothing.
func TestInitializeWithoutInstructionsLeavesNoDescription(t *testing.T) {
	tr := &instructionsTransport{raw: json.RawMessage(`{"capabilities":{"tools":{}}}`)}
	c := &Client{name: "quiet", t: tr, spec: Spec{Name: "quiet"}}
	if err := c.initialize(context.Background()); err != nil {
		t.Fatalf("initialize: %v", err)
	}
	if c.instructions != "" {
		t.Fatalf("instructions = %q, want empty", c.instructions)
	}
}

// The description has to survive the schema cache, or a server that is
// configured but not connected loses the only thing that explains it.
func TestCachedSchemaCarriesInstructions(t *testing.T) {
	redirectCache(t)
	spec := sampleSpec()
	cs := sampleCachedSchema(SchemaCacheKey(spec))
	cs.Instructions = "Talks to the build cluster."
	if err := SaveCachedSchema(spec.Name, cs); err != nil {
		t.Fatalf("SaveCachedSchema: %v", err)
	}
	got, ok := LoadCachedSchemaForSpec(spec)
	if !ok {
		t.Fatal("LoadCachedSchemaForSpec found nothing")
	}
	if got.Instructions != cs.Instructions {
		t.Fatalf("Instructions = %q, want %q", got.Instructions, cs.Instructions)
	}
}
