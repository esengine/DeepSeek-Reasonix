package boot

import (
	"context"
	"encoding/json"
	"os"
	"testing"

	"reasonix/internal/ablation"
	"reasonix/internal/agent"
	"reasonix/internal/event"
	"reasonix/internal/provider"
)

// TestEffectSessionLogUpgradeKeepsModelMessagesThroughRealBuild opens a
// schema-1 session written by the real stack, upgrades it on the next save,
// and pins that the provider-visible transcript (ids included) is
// byte-identical across the upgrade: the format change never touches the
// prompt-cache prefix.
func TestEffectSessionLogUpgradeKeepsModelMessagesThroughRealBuild(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	t.Setenv("REASONIX_SESSION_LOG", "v1")

	rec := &effectRecordingProvider{}
	provider.Register("boot-effect-session-log", func(provider.Config) (provider.Provider, error) {
		return rec, nil
	})
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
	first, err := Build(context.Background(), Options{Sink: event.Discard, Ablation: ablation.Set{}})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	first.EnsureSessionPath()
	if err := first.Run(context.Background(), "reply ok"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	path := first.SessionPath()
	before, err := json.Marshal(provider.ModelMessages(first.History()))
	if err != nil {
		t.Fatal(err)
	}
	first.Close()
	if heads, err := agent.ListSessionHeads(path); err != nil || heads != nil {
		t.Fatalf("schema-1 session must have no heads yet: %v %v", heads, err)
	}

	if err := os.Unsetenv("REASONIX_SESSION_LOG"); err != nil {
		t.Fatal(err)
	}
	lease, err := agent.TryAcquireSessionLease(path)
	if err != nil {
		t.Fatalf("lease: %v", err)
	}
	defer lease.Release()
	loaded, err := agent.LoadSession(path)
	if err != nil {
		t.Fatalf("LoadSession: %v", err)
	}
	second, err := Build(context.Background(), Options{Sink: event.Discard, Ablation: ablation.Set{}})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer second.Close()
	second.Resume(loaded, path)
	if err := second.Run(context.Background(), "reply again"); err != nil {
		t.Fatalf("Run after resume: %v", err)
	}
	if err := second.Snapshot(); err != nil {
		t.Fatalf("Snapshot: %v", err)
	}
	heads, err := agent.ListSessionHeads(path)
	if err != nil || len(heads) != 1 {
		t.Fatalf("session was not upgraded to schema 2: heads=%v err=%v", heads, err)
	}
	after, err := json.Marshal(provider.ModelMessages(second.History()))
	if err != nil {
		t.Fatal(err)
	}
	var beforeMsgs, afterMsgs []json.RawMessage
	if err := json.Unmarshal(before, &beforeMsgs); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(after, &afterMsgs); err != nil {
		t.Fatal(err)
	}
	if len(afterMsgs) <= len(beforeMsgs) {
		t.Fatalf("resumed transcript did not grow: before %d after %d", len(beforeMsgs), len(afterMsgs))
	}
	for i := range beforeMsgs {
		if string(beforeMsgs[i]) != string(afterMsgs[i]) {
			t.Fatalf("message %d changed across the schema upgrade\nbefore: %s\nafter:  %s", i, beforeMsgs[i], afterMsgs[i])
		}
	}
	reloaded, err := agent.LoadSession(path)
	if err != nil || len(reloaded.Messages) != len(afterMsgs) {
		t.Fatalf("reload after upgrade: err=%v len=%d want %d", err, len(reloaded.Messages), len(afterMsgs))
	}
}
