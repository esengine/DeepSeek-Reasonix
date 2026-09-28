package boot

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/ablation"
	"reasonix/internal/agent"
	"reasonix/internal/event"
	"reasonix/internal/provider"
)

// TestEffectSessionLogUpgradeKeepsModelMessagesThroughRealBuild freezes the
// production compatibility boundary: continuing a schema-1 transcript imports
// it into a final identity-bound v3 session, leaves the source bytes untouched,
// and preserves the provider-visible prefix (including stable message ids).
func TestEffectSessionLogUpgradeKeepsModelMessagesThroughRealBuild(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	t.Setenv(agent.SessionLogSchemaEnv, "v1")

	rec := &effectRecordingProvider{}
	provider.Register("boot-effect-session-log", func(provider.Config) (provider.Provider, error) { return rec, nil })
	writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"

[agent]
system_prompt = "BASE"

[environment]
enabled = false

[[providers]]
name = "test-model"
kind = "boot-effect-session-log"
model = "x"
`)
	approveWorkspace(t, dir)

	legacyDir := filepath.Join(t.TempDir(), "sessions")
	legacyPath := agent.NewSessionPath(legacyDir, "legacy")
	legacy := agent.NewSession("BASE")
	legacy.Add(provider.Message{Role: provider.RoleUser, Content: "reply ok"})
	legacy.Add(provider.Message{Role: provider.RoleAssistant, Content: "ok"})
	if err := legacy.Save(legacyPath); err != nil {
		t.Fatalf("write schema-1 source: %v", err)
	}
	beforeSource, err := os.ReadFile(legacyPath)
	if err != nil {
		t.Fatal(err)
	}
	before, err := json.Marshal(provider.ModelMessages(legacy.Snapshot()))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Unsetenv(agent.SessionLogSchemaEnv); err != nil {
		t.Fatal(err)
	}

	ctrl, err := Build(context.Background(), withTestSession(t, Options{SessionDir: legacyDir, Sink: event.Discard, Ablation: ablation.Set{}}))
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	ref, err := ctrl.ContinueLegacySession(context.Background(), legacyPath, "")
	if err != nil {
		ctrl.Close()
		t.Fatalf("ContinueLegacySession: %v", err)
	}
	if ctrl.SessionPath() != "" {
		t.Fatalf("migrated v3 runtime retained legacy execution path %q", ctrl.SessionPath())
	}
	if err := ctrl.Run(context.Background(), "reply again"); err != nil {
		ctrl.Close()
		t.Fatalf("Run after migration: %v", err)
	}
	after, err := json.Marshal(provider.ModelMessages(ctrl.History()))
	if err != nil {
		t.Fatal(err)
	}
	service, runtime, ok := ctrl.SessionBinding()
	if !ok || runtime.Ref() != ref {
		t.Fatalf("bound runtime = %+v, want %+v", runtime, ref)
	}
	ctrl.Close()

	var beforeMsgs, afterMsgs []json.RawMessage
	if err := json.Unmarshal(before, &beforeMsgs); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(after, &afterMsgs); err != nil {
		t.Fatal(err)
	}
	if len(afterMsgs) <= len(beforeMsgs) {
		t.Fatalf("migrated transcript did not grow: before %d after %d", len(beforeMsgs), len(afterMsgs))
	}
	for i := range beforeMsgs {
		if string(beforeMsgs[i]) != string(afterMsgs[i]) {
			t.Fatalf("message %d changed across legacy import\nbefore: %s\nafter:  %s", i, beforeMsgs[i], afterMsgs[i])
		}
	}
	if got, err := os.ReadFile(legacyPath); err != nil || !bytes.Equal(got, beforeSource) {
		t.Fatalf("legacy source changed during import: err=%v", err)
	}
	cold, err := service.Query().History(context.Background(), ref)
	if err != nil {
		t.Fatalf("cold v3 history: %v", err)
	}
	if len(provider.ModelMessages(cold)) != len(afterMsgs) {
		t.Fatalf("cold v3 history len=%d, want %d", len(provider.ModelMessages(cold)), len(afterMsgs))
	}
}
