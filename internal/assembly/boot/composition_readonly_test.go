package boot

import (
	"errors"
	"testing"

	"reasonix/internal/contract/agentcomposition"
	"reasonix/internal/state/agentpreset"
)

func saveReadOnlyComposition(t *testing.T, home, root string) {
	t.Helper()
	d := agentcomposition.Builtins()[0]
	d.ID, d.Name = "user:review", "Read-only review"
	d.Persona.Prefix = "You are a rigorous code reviewer. Read the implementation, identify reachable defects, and support findings with evidence."
	d.Tools = agentcomposition.Selection{Mode: agentcomposition.Select, Members: []agentcomposition.Module{{ID: "read_file"}, {ID: "ls"}, {ID: "grep"}, {ID: "glob"}}}
	empty := agentcomposition.Selection{Mode: agentcomposition.Select, Members: []agentcomposition.Module{}}
	d.Skills, d.MCP, d.Extensions, d.Hooks, d.Commands = empty, empty, empty, empty, empty
	if _, err := agentpreset.New(home, root).Save(d, ""); err != nil {
		t.Fatal(err)
	}
}

func TestRetiredBuiltinBindingRestoresWithoutItsCatalogEntry(t *testing.T) {
	home, root := compositionTestWorkspace(t, "openai")
	saveReadOnlyComposition(t, home, root)
	first, err := BuildRuntime(t.Context(), Options{Home: home, WorkspaceRoot: root, AgentComposition: "user:review", resolvedShell: pinnedEffectShell()})
	if err != nil {
		t.Fatal(err)
	}
	defer first.Controller.Close()
	binding := first.Controller.CompositionBinding()
	// A prior built-in writer retained this self-contained binding before its catalog entry retired.
	binding.Version.Definition.ID, binding.Resolved.ID = "builtin:review", "builtin:review"
	binding.Version.Revision = agentcomposition.Revision(binding.Version.Definition)
	binding.Seal()
	store := agentpreset.New(home, root)
	if err := store.RetainBinding(binding); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Get("builtin:review"); !errors.Is(err, agentpreset.ErrNotFound) {
		t.Fatalf("retired mode remains available for new bindings: %v", err)
	}
	restored, err := BuildRuntime(t.Context(), Options{Home: home, WorkspaceRoot: root, Composition: binding, resolvedShell: pinnedEffectShell()})
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Controller.Close()
	if restored.Controller.CompositionBinding().Digest != binding.Digest || restored.Snapshot.CacheHash() != first.Snapshot.CacheHash() {
		t.Fatal("retired binding changed on restore")
	}
}
