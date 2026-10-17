package agent

import (
	"context"
	"strings"
	"testing"

	"reasonix/internal/contract/agentcomposition"
	"reasonix/internal/contract/tool"
	"reasonix/internal/state/sessionstore"
)

func TestChildCompositionKeepsExactBindingAndIntersectsProfileCeiling(t *testing.T) {
	d := agentcomposition.Builtins()[0]
	d.ID = "user:reader"
	d.Tools = agentcomposition.Selection{Mode: agentcomposition.Select, Members: []agentcomposition.Module{{ID: "read_file"}, {ID: "grep"}}}
	d.Persona.Prefix = "PARENT PERSONA"
	b := &agentcomposition.Binding{Resolved: d, Version: agentcomposition.Version{Definition: d, Revision: agentcomposition.Revision(d)}}
	b.Seal()
	ctx := context.WithValue(t.Context(), compositionContextKey{}, b)
	s := sessionstore.NewSession("child instructions")
	if err := BindChildComposition(ctx, s, true); err != nil {
		t.Fatal(err)
	}
	if s.Composition().Digest != b.Digest || !strings.Contains(s.Snapshot()[0].Content, "PARENT PERSONA") {
		t.Fatal("child lost binding or persona")
	}
	reg := tool.NewRegistry()
	reg.Add(namedSchemaTool{name: "read_file", schema: `{"type":"object"}`})
	reg.Add(namedSchemaTool{name: "bash", schema: `{"type":"object"}`})
	restrictChildComposition(ctx, reg)
	for _, name := range []string{"bash", "write_file", "grep"} {
		reg.Add(namedSchemaTool{name: name, schema: `{"type":"object"}`})
		if _, ok := reg.Get(name); ok {
			t.Fatalf("child widened through %s", name)
		}
	}
	if _, ok := reg.Get("read_file"); !ok {
		t.Fatal("intersection dropped allowed reader")
	}
	other := agentcomposition.Clone(b)
	other.Resolved.Persona.Prefix = "OTHER"
	other.Seal()
	if err := BindChildComposition(context.WithValue(ctx, compositionContextKey{}, other), s, true); err == nil {
		t.Fatal("continued child accepted another binding")
	}
}
