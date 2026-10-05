package tool

import (
	"context"
	"encoding/json"
	"testing"

	"reasonix/internal/contract/provider"
)

type surfaceTool struct {
	name string
}

func (s surfaceTool) Name() string            { return s.name }
func (s surfaceTool) Description() string     { return s.name }
func (s surfaceTool) ReadOnly() bool          { return true }
func (s surfaceTool) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (s surfaceTool) Execute(context.Context, json.RawMessage) (string, error) {
	return "", nil
}

type surfaceKey struct{}

type gatedTool struct {
	surfaceTool
}

func (gatedTool) ProviderVisible(ctx context.Context) bool {
	return ctx.Value(surfaceKey{}) != nil
}

func (gatedTool) Unavailable(context.Context) Refusal {
	return Refusal{Code: "surface.closed", Message: "closed"}
}

func surfaceRegistry(t *testing.T) *Registry {
	t.Helper()
	r := NewRegistry()
	for _, name := range []string{"alpha", "zulu", "mike"} {
		r.Add(surfaceTool{name: name})
	}
	for _, name := range []string{"beta_gate", "yankee_gate"} {
		r.Add(gatedTool{surfaceTool{name: name}})
	}
	r.SetProviderVisibleTools([]string{"alpha", "zulu", "mike", "beta_gate", "yankee_gate"})
	return r
}

func namesOf(schemas []provider.ToolSchema) []string {
	out := make([]string, 0, len(schemas))
	for _, s := range schemas {
		out = append(out, s.Name)
	}
	return out
}

func TestProviderSchemasPutsEveryContextualToolAfterEveryStableOne(t *testing.T) {
	r := surfaceRegistry(t)
	got := namesOf(r.ProviderSchemas(context.WithValue(context.Background(), surfaceKey{}, true)))
	want := []string{"alpha", "mike", "zulu", "beta_gate", "yankee_gate"}
	if len(got) != len(want) {
		t.Fatalf("surface = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("surface = %v, want %v — a contextual tool ahead of a stable one breaks the prefix", got, want)
		}
	}
}

// The absence of a contextual tool has to be a prefix of its presence: that is
// what lets the cache keep every token before the boundary when one comes or
// goes. Measured against DeepSeek: appending at the tail cost nothing, while
// the same tool inserted earlier cost 1024 cached tokens.
func TestAbsentSurfaceIsAPrefixOfThePresentOne(t *testing.T) {
	r := surfaceRegistry(t)
	closed := namesOf(r.ProviderSchemas(context.Background()))
	open := namesOf(r.ProviderSchemas(context.WithValue(context.Background(), surfaceKey{}, true)))
	if len(closed) >= len(open) {
		t.Fatalf("closed surface %v is not shorter than open %v; this arm measures nothing", closed, open)
	}
	for i, name := range closed {
		if open[i] != name {
			t.Fatalf("position %d differs: closed=%v open=%v — the shared head is what the cache keeps", i, closed, open)
		}
	}
}

func TestStableSegmentIsIdenticalWhateverTheContext(t *testing.T) {
	r := surfaceRegistry(t)
	closed := namesOf(r.ProviderSchemas(context.Background()))
	open := namesOf(r.ProviderSchemas(context.WithValue(context.Background(), surfaceKey{}, true)))
	for i, name := range []string{"alpha", "mike", "zulu"} {
		if closed[i] != name || open[i] != name {
			t.Fatalf("stable segment moved with the context: closed=%v open=%v", closed, open)
		}
	}
}
