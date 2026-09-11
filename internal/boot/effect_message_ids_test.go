package boot

import (
	"context"
	"testing"

	"reasonix/internal/ablation"
	"reasonix/internal/agent"
	"reasonix/internal/event"
	"reasonix/internal/provider"
)

// withoutMessageIDs strips the per-session local ids so two independent runs
// can be compared on provider-visible content.
func withoutMessageIDs(msgs []provider.Message) []provider.Message {
	out := append([]provider.Message(nil), msgs...)
	for i := range out {
		out[i].ID = ""
	}
	return out
}

// TestEffectMessageIDsPersistThroughRealBuild pins the identity contract at
// both boundaries the real stack crosses: every message handed to the provider
// carries an id, and the transcript reloaded from disk carries the same ids.
func TestEffectMessageIDsPersistThroughRealBuild(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)

	rec := &effectRecordingProvider{}
	provider.Register("boot-effect-message-ids", func(provider.Config) (provider.Provider, error) {
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
kind = "boot-effect-message-ids"
model = "x"
`)
	ctrl, err := Build(context.Background(), Options{Sink: event.Discard, Ablation: ablation.Set{}})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	ctrl.EnsureSessionPath()
	if err := ctrl.Run(context.Background(), "reply ok"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	path := ctrl.SessionPath()
	ctrl.Close()
	reqs := rec.requests()
	if len(reqs) == 0 {
		t.Fatal("no request reached the provider boundary")
	}
	sent := make(map[string]string)
	for _, m := range reqs[len(reqs)-1].Messages {
		if m.ID == "" {
			t.Fatalf("message %q reached the provider boundary without an id", m.Content)
		}
		sent[m.ID] = m.Content
	}
	loaded, err := agent.LoadSession(path)
	if err != nil {
		t.Fatalf("LoadSession(%s): %v", path, err)
	}
	matched := 0
	for _, m := range loaded.Messages {
		if m.ID == "" {
			t.Fatalf("persisted message %q lost its id", m.Content)
		}
		if content, ok := sent[m.ID]; ok {
			if content != m.Content {
				t.Fatalf("id %s maps to %q in the request but %q on disk", m.ID, content, m.Content)
			}
			matched++
		}
	}
	if matched == 0 {
		t.Fatalf("no request message id survived the save/load round trip (sent %d, loaded %d)", len(sent), len(loaded.Messages))
	}
}
