package boot

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/agentcomposition"
	"reasonix/internal/contract/config"
	"reasonix/internal/state/agentpreset"
)

func TestCompositionRetainsLinkedSkillAcrossRestart(t *testing.T) {
	for _, layout := range []string{"directory", "file"} {
		t.Run(layout, func(t *testing.T) {
			home, root := compositionTestWorkspace(t, "openai")
			source := testenv.TempDir(t)
			writeFile(t, source, "SKILL.md", "---\nname: linked\ndescription: linked skill\n---\nORIGINAL SKILL")
			writeFile(t, source, "references/guide.md", "ORIGINAL REFERENCE")
			target, link := source, filepath.Join(home, "skills", "linked")
			if layout == "file" {
				target, link = filepath.Join(source, "SKILL.md"), link+".md"
			}
			if err := os.MkdirAll(filepath.Dir(link), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, link); err != nil {
				t.Skipf("symlink creation unavailable: %v", err)
			}
			d := agentcomposition.Builtins()[0]
			d.ID, d.Persona.Prefix = "user:linked", "Custom persona"
			if _, err := agentpreset.New(home, root).Save(d, ""); err != nil {
				t.Fatal(err)
			}
			opts := Options{Home: home, WorkspaceRoot: root, AgentComposition: d.ID, resolvedShell: pinnedEffectShell()}
			first, err := buildSnapshotRuntime(t.Context(), opts)
			if err != nil {
				t.Fatal(err)
			}
			defer first.Controller.Close()
			binding := first.Controller.CompositionBinding()
			if binding == nil {
				t.Fatal("custom persona was not bound")
			}
			if err := os.RemoveAll(source); err != nil {
				t.Fatal(err)
			}
			opts.AgentComposition, opts.Composition = "", binding
			restored, err := buildSnapshotRuntime(t.Context(), opts)
			if err != nil {
				t.Fatal(err)
			}
			defer restored.Controller.Close()
			if first.Snapshot.CacheHash() != restored.Snapshot.CacheHash() {
				t.Fatal("restoring the linked skill changed the provider prefix")
			}
			found := false
			for _, sk := range restored.Controller.Skills() {
				if sk.Name != "linked" {
					continue
				}
				found = true
				if layout == "directory" {
					data, err := os.ReadFile(filepath.Join(filepath.Dir(sk.Path), "references", "guide.md"))
					if err != nil || string(data) != "ORIGINAL REFERENCE" {
						t.Fatalf("linked skill reference not retained: %q %v", data, err)
					}
				}
			}
			if !found {
				t.Fatal("linked skill was lost")
			}
			if err := config.DefaultActivationStore().SetSkillEnabled("linked", root, config.ActivationProject, false); err != nil {
				t.Fatal(err)
			}
			if err := restored.Controller.CompositionExecutionError(); !errors.Is(err, agentcomposition.ErrUnavailable) {
				t.Fatalf("linked skill revocation ignored: %v", err)
			}
		})
	}
}

func TestCompositionLinkedSkillRejectsLinkedResources(t *testing.T) {
	home, root := compositionTestWorkspace(t, "openai")
	source, outside := testenv.TempDir(t), testenv.TempDir(t)
	writeFile(t, source, "SKILL.md", "---\nname: linked\ndescription: linked skill\n---\nSKILL")
	writeFile(t, outside, "private.txt", "must not be retained")
	if err := os.Symlink(filepath.Join(outside, "private.txt"), filepath.Join(source, "reference.md")); err != nil {
		t.Skipf("symlink creation unavailable: %v", err)
	}
	link := filepath.Join(home, "skills", "linked")
	if err := os.MkdirAll(filepath.Dir(link), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(source, link); err != nil {
		t.Fatal(err)
	}
	d := agentcomposition.Builtins()[0]
	d.ID = "user:linked"
	if _, err := agentpreset.New(home, root).Save(d, ""); err != nil {
		t.Fatal(err)
	}
	result, err := buildSnapshotRuntime(t.Context(), Options{Home: home, WorkspaceRoot: root, AgentComposition: d.ID, resolvedShell: pinnedEffectShell()})
	if result != nil {
		result.Controller.Close()
	}
	if !errors.Is(err, agentcomposition.ErrUnavailable) {
		t.Fatalf("linked resource was not refused: %v", err)
	}
}
