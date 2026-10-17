package boot

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/contract/agentcomposition"
	"reasonix/internal/contract/config"
	"reasonix/internal/ext/hook"
	"reasonix/internal/state/agentpreset"
)

func TestCompositionRetainsPersonaSkillsCommandsAndHooksAcrossSourceDeletion(t *testing.T) {
	home, root := compositionTestWorkspace(t, "openai")
	skillPath := filepath.Join(root, ".reasonix", "skills", "frozen", "SKILL.md")
	commandPath := filepath.Join(root, ".reasonix", "commands", "frozen.md")
	writeFile(t, root, ".reasonix/skills/frozen/SKILL.md", "---\nname: frozen\ndescription: frozen skill\nuser-invocable: false\n---\nORIGINAL SKILL")
	writeFile(t, root, ".reasonix/commands/frozen.md", "ORIGINAL COMMAND")
	script := filepath.Join(home, "scripts", "startup.txt")
	writeFile(t, home, "scripts/startup.txt", "ORIGINAL HOOK")
	settings := hook.Settings{Hooks: map[hook.Event][]hook.HookConfig{hook.SessionStart: {{Command: "cat " + script}}}}
	raw, _ := json.Marshal(settings)
	writeFile(t, home, hook.SettingsFilename, string(raw))
	store := agentpreset.New(home, root)
	d := agentcomposition.Builtins()[0]
	d.ID = "user:frozen"
	d.Persona.Prefix = "ORIGINAL PERSONA"
	version, err := store.Save(d, "")
	if err != nil {
		t.Fatal(err)
	}
	opts := Options{Home: home, WorkspaceRoot: root, Model: "test-model", AgentComposition: d.ID, resolvedShell: pinnedEffectShell()}
	first, err := BuildRuntime(t.Context(), opts)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Controller.Close()
	if got := first.Controller.HookRunner().SessionStart(t.Context()); len(got) != 1 || got[0] != "ORIGINAL HOOK" {
		t.Fatalf("initial hook=%q", got)
	}
	if len(first.Controller.Skills()) == 0 {
		t.Fatal("model-only skill omitted")
	}
	for _, path := range []string{skillPath, commandPath, script} {
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
	}
	d.Persona.Prefix = "NEW PERSONA"
	updated, err := store.Save(d, version.Revision)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Delete(d.ID, updated.Revision); err != nil {
		t.Fatal(err)
	}
	if err := first.Controller.ReloadCommands(t.Context()); err != nil {
		t.Fatal(err)
	}
	if got := first.Controller.Commands(); len(got) != 1 || !strings.Contains(got[0].Body, "ORIGINAL COMMAND") {
		t.Fatal("reload followed command source")
	}
	opts.AgentComposition, opts.Composition = "", first.Controller.CompositionBinding()
	restored, err := BuildRuntime(t.Context(), opts)
	if err != nil {
		t.Fatal(err)
	}
	defer restored.Controller.Close()
	if restored.Controller.CompositionBinding().Digest != opts.Composition.Digest || !strings.Contains(restored.Snapshot.SystemPrompt(), "ORIGINAL PERSONA") {
		t.Fatal("restoration followed definition edit")
	}
	found := false
	for _, sk := range restored.Controller.Skills() {
		if sk.Name == "frozen" && strings.Contains(sk.Body, "ORIGINAL SKILL") {
			found = true
		}
	}
	if !found {
		t.Fatal("deleted skill source lost")
	}
	if got := restored.Controller.HookRunner().SessionStart(t.Context()); len(got) != 1 || got[0] != "ORIGINAL HOOK" {
		t.Fatalf("restored hook=%q", got)
	}
	if _, err := BuildRuntime(t.Context(), Options{Home: home, WorkspaceRoot: root, Model: "test-model", AgentComposition: d.ID, resolvedShell: pinnedEffectShell()}); !errors.Is(err, agentpreset.ErrNotFound) {
		t.Fatalf("deleted definition remained selectable: %v", err)
	}
	if err := config.DefaultActivationStore().SetSkillEnabled("frozen", root, config.ActivationProject, false); err != nil {
		t.Fatal(err)
	}
	if err := restored.Controller.CompositionExecutionError(); !errors.Is(err, agentcomposition.ErrUnavailable) {
		t.Fatalf("revocation ignored: %v", err)
	}
}
