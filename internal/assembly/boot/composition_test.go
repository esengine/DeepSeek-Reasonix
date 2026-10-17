package boot

import (
	"context"
	"testing"

	"reasonix/internal/contract/provider"
)

func TestCompositionRestrictsRealRegistryAndRetainsBinding(t *testing.T) {
	home := isolateConfigHome(t)
	root := robustTempDir(t)
	writeFile(t, root, "reasonix.toml", `default_model = "test-model"
[[providers]]
name = "test-model"
kind = "openai"
base_url = "https://example.invalid"
model = "x"
`)
	approveWorkspaceAt(t, home, root)
	saveReadOnlyComposition(t, home, root)
	first, err := BuildRuntime(context.Background(), Options{Home: home, WorkspaceRoot: root, AgentComposition: "user:review", resolvedShell: pinnedEffectShell()})
	if err != nil {
		t.Fatal(err)
	}
	defer first.Controller.Close()
	for _, name := range first.Assembly.Registry.AllNames() {
		if name == "bash" || name == "task" || name == "write_file" || name == "install_skill" {
			t.Fatalf("restricted tool %s registered", name)
		}
	}
	b := first.Controller.CompositionBinding()
	if b == nil || b.Version.Definition.ID != "user:review" {
		t.Fatalf("binding missing: %+v", b)
	}
	second, err := BuildRuntime(context.Background(), Options{Home: home, WorkspaceRoot: root, Composition: b, resolvedShell: pinnedEffectShell()})
	if err != nil {
		t.Fatal(err)
	}
	defer second.Controller.Close()
	if first.Snapshot.CacheHash() != second.Snapshot.CacheHash() {
		t.Fatal("pinned rebuild changed provider prefix")
	}
	for _, m := range provider.ModelMessages(first.Controller.History()) {
		if m.AgentComposition != nil {
			t.Fatal("binding leaked")
		}
	}
}
