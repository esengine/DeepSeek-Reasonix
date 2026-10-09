package boot

import (
	"context"
	"os"
	"slices"
	"strings"
	"sync"
	"testing"

	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
)

// A settings verb typed into a built controller must reach the frontend sink as
// a notice, persist through the real config path, and never become a model turn.
func TestEffectSettingsSlashStaysOffTheModelAndReachesTheSink(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	rec := &effectRecordingProvider{}
	provider.Register("boot-settings-slash", func(provider.Config) (provider.Provider, error) { return rec, nil })
	writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"

[[providers]]
name = "test-model"
kind = "boot-settings-slash"
model = "x"
`)
	approveWorkspace(t, dir)

	var mu sync.Mutex
	var notices []string
	sink := event.FuncSink(func(e event.Event) {
		if e.Kind == event.Notice {
			mu.Lock()
			notices = append(notices, e.Text)
			mu.Unlock()
		}
	})
	ctrl, err := Build(context.Background(), Options{Sink: sink})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()

	if err := ctrl.Run(context.Background(), "hello"); err != nil {
		t.Fatalf("first turn: %v", err)
	}
	before := agentRequests(rec.requests())
	for _, line := range []string{"/sandbox", "/output-style", "/reasoning-language zh", "/currency usd"} {
		ctrl.Submit(line)
	}
	if err := ctrl.Run(context.Background(), "hello"); err != nil {
		t.Fatalf("second turn: %v", err)
	}
	mu.Lock()
	joined := strings.Join(notices, "\n---\n")
	mu.Unlock()
	for _, want := range []string{"OS bash sandbox", "output styles", "reasoning-language set to zh"} {
		if !strings.Contains(joined, want) {
			t.Errorf("sink never saw %q:\n%s", want, joined)
		}
	}
	after := agentRequests(rec.requests())
	if len(before) != 1 || len(after) != 2 {
		t.Fatalf("slash verbs reached the model: %d requests before, %d after two turns", len(before), len(after))
	}
	first, second := before[0], after[1]
	if systemOf(first) != systemOf(second) {
		t.Error("changing the reasoning language moved the cache-stable system prefix")
	}
	if lastUserOf(first) == lastUserOf(second) || !strings.Contains(lastUserOf(second), "hello") {
		t.Errorf("the reasoning language never reached the next request:\nbefore: %q\nafter:  %q", lastUserOf(first), lastUserOf(second))
	}
	raw, err := os.ReadFile(config.UserConfigPath())
	if err != nil {
		t.Fatalf("read stored config: %v", err)
	}
	if !strings.Contains(string(raw), "USD") || !strings.Contains(string(raw), "zh") {
		t.Errorf("settings were not stored:\n%s", raw)
	}
}

func lastUserOf(req provider.Request) string {
	for _, m := range slices.Backward(req.Messages) {
		if m.Role == "user" {
			return m.Content
		}
	}
	return ""
}
