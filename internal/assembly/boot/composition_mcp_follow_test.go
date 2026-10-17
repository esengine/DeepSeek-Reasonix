package boot

import (
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	"reasonix/internal/contract/agentcomposition"
	"reasonix/internal/contract/provider"
)

func TestCompositionFollowingMCPChangesCannotRewriteAnExistingBinding(t *testing.T) {
	home := filepath.Join(isolateConfigHome(t), ".reasonix")
	t.Setenv("REASONIX_HOME", home)
	root := robustTempDir(t)
	t.Chdir(root)
	server := newDriftingMCPServer(t)
	rec := &capabilityCallProvider{direct: "mcp__drift__extra"}
	provider.Register("composition-follow-guard", func(provider.Config) (provider.Provider, error) { return rec, nil })
	writeFile(t, root, "reasonix.toml", fmt.Sprintf(`
default_model = "test-model"
[codegraph]
enabled = false
[[providers]]
name = "test-model"
kind = "composition-follow-guard"
model = "x"
[[plugins]]
name = "drift"
type = "http"
url = %q
load = "always"
`, server.URL))
	approveWorkspace(t, root)
	approveProjectServer(t, root, "drift")
	opts := Options{Home: home, WorkspaceRoot: root, AgentComposition: "builtin:general", resolvedShell: pinnedEffectShell()}
	first, err := BuildRuntime(t.Context(), opts)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Controller.Close()
	binding := first.Controller.CompositionBinding()
	if _, err := first.Controller.SetMCPFollowToolChanges("drift", true); err != nil {
		t.Fatal(err)
	}
	server.set(rewrittenDescription, true)
	if _, err := first.Controller.ReconnectMCPServer("drift"); err == nil {
		t.Fatal("following rewrote an existing tool contract")
	}
	first.Controller.EnsureSessionPath()
	if err := first.Controller.RunTurn(t.Context(), "Use the newly added tool"); !errors.Is(err, agentcomposition.ErrUnavailable) {
		t.Fatalf("changed binding reached turn execution: %v", err)
	}
	if len(rec.requests()) != 0 || server.called("extra") || first.Controller.CompositionBinding().Digest != binding.Digest {
		t.Fatal("a followed change reached the provider or rewrote the binding")
	}
	fresh, err := BuildRuntime(t.Context(), opts)
	if err != nil {
		t.Fatal(err)
	}
	defer fresh.Controller.Close()
	if fresh.Controller.CompositionBinding().Digest == binding.Digest {
		t.Fatal("new conversation reused the obsolete contract")
	}
	if _, ok := fresh.Assembly.Registry.Get("mcp__drift__extra"); !ok {
		t.Fatal("new conversation lost the followed server's tool")
	}
}
