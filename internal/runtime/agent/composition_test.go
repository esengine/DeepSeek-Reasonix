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

func TestCarryChildCompositionCopiesOnlyFrozenBinding(t *testing.T) {
	d := agentcomposition.Builtins()[0]
	d.Persona.Prefix = "FROZEN PERSONA"
	b := &agentcomposition.Binding{Resolved: d, Version: agentcomposition.Version{Definition: d, Revision: agentcomposition.Revision(d)}}
	b.Seal()
	type unrelatedKey struct{}
	source := context.WithValue(context.WithValue(t.Context(), compositionContextKey{}, b), unrelatedKey{}, "host capability")
	ctx, cancel := context.WithCancel(t.Context())
	child := CarryChildComposition(ctx, source)
	s := sessionstore.NewSession("child instructions")
	if err := BindChildComposition(child, s, true); err != nil {
		t.Fatal(err)
	}
	if s.Composition().Digest != b.Digest || !strings.Contains(s.Snapshot()[0].Content, "FROZEN PERSONA") {
		t.Fatal("child lost binding or persona")
	}
	b.Resolved.Persona.Prefix = "MUTATED"
	if !strings.Contains(ChildCompositionPrompt(child, "base"), "FROZEN PERSONA") {
		t.Fatal("child retained mutable source binding")
	}
	if child.Value(unrelatedKey{}) != nil {
		t.Fatal("copied unrelated host capability")
	}
	cancel()
	if child.Err() != context.Canceled {
		t.Fatal("lost job cancellation")
	}
}
