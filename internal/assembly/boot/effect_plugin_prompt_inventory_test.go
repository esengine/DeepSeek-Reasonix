package boot

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/ext/pluginpkg"
	"reasonix/internal/runtime/agent/testutil"
)

func TestEffectDisplayedPluginPromptInvocationReachesProvider(t *testing.T) {
	registerBootTokenProfileTestProvider()
	home := isolateConfigHome(t)
	reasonixHome := filepath.Join(home, ".reasonix")
	t.Setenv("REASONIX_HOME", reasonixHome)
	workspace := robustTempDir(t)
	t.Chdir(workspace)
	writeFile(t, workspace, "reasonix.toml", `
default_model = "test-model"
[agent]
system_prompt = "PROMPT INVENTORY BASE"
[environment]
enabled = false
[codegraph]
enabled = false
[[providers]]
name = "test-model"
kind = "boot-token-profile-test"
model = "x"
`)
	approveWorkspace(t, workspace)
	root := pluginpkg.InstallRoot(reasonixHome, "notes-kit")
	writeFile(t, root, pluginpkg.NativeManifest, `{"apiVersion":"reasonix.io/plugin/v2","name":"notes-kit","contributes":{"prompts":["prompts"]}}`)
	writeFile(t, root, "prompts/review/brief.md", "---\ndescription: Summarize a note\nargument-hint: <note>\n---\nPACKAGE PROMPT: $ARGUMENTS")
	if err := pluginpkg.Upsert(reasonixHome, pluginpkg.InstalledPlugin{Name: "notes-kit", Root: pluginpkg.RelativeRoot(reasonixHome, root), ManifestKind: "reasonix", Enabled: true}); err != nil {
		t.Fatal(err)
	}
	out, err := pluginpkg.InstalledShowText(reasonixHome, "notes-kit")
	if err != nil {
		t.Fatal(err)
	}
	_, prompts, ok := strings.Cut(out, "prompts:\n")
	if !ok || len(strings.Fields(prompts)) == 0 {
		t.Fatalf("prompt inventory missing: %s", out)
	}
	invocation := strings.Fields(prompts)[0]
	rec := testutil.NewMock("prompt-inventory", testutil.Turn{Text: "ok"})
	setBootTokenProfileTestProvider(t, rec)
	ctrl, err := Build(t.Context(), Options{Sink: event.Discard, SessionDir: filepath.Join(robustTempDir(t), "sessions")})
	if err != nil {
		t.Fatal(err)
	}
	defer ctrl.Close()
	ctrl.EnsureSessionPath()
	ctrl.Submit(invocation + " selected note")
	deadline := time.Now().Add(30 * time.Second)
	for ctrl.Running() {
		if time.Now().After(deadline) {
			t.Fatal("displayed prompt turn did not finish")
		}
		time.Sleep(time.Millisecond)
	}
	requests := agentRequests(rec.Requests())
	if len(requests) != 1 {
		t.Fatalf("provider requests=%d, want one", len(requests))
	}
	var user string
	for _, message := range requests[0].Messages {
		if message.Role == provider.RoleUser {
			user = message.Content
		}
	}
	if !strings.Contains(user, "PACKAGE PROMPT: selected note") {
		t.Fatalf("displayed invocation %q failed to render at provider boundary: %s", invocation, user)
	}
	if strings.Contains(systemMessage(requests[0].Messages), "PACKAGE PROMPT:") {
		t.Fatal("prompt template entered the cache-stable prefix")
	}
}
