package boot

import (
	"context"
	"strings"
	"testing"

	"reasonix/internal/event"

	_ "reasonix/internal/provider/openai"
	_ "reasonix/internal/tool/builtin"
)

// TestBuildUnknownModelErrorIsActionable: a default_model that doesn't resolve
// (e.g. a preset name after [[providers]] replaced the built-in presets) must
// fail with a message that names the model, lists what IS configured, and hints
// at the [[providers]] trap — not a silent empty model.
func TestBuildUnknownModelErrorIsActionable(t *testing.T) {
	dir := robustTempDir(t)
	t.Chdir(dir)
	writeFile(t, dir, "reasonix.toml", `
default_model = "mimo"

[codegraph]
enabled = false

[[providers]]
name = "deepseek-flash"
kind = "openai"
base_url = "https://example.invalid"
model = "deepseek-v4-flash"
api_key_env = "REASONIX_TEST_KEY_UNSET"
`)

	_, err := Build(context.Background(), Options{Sink: event.Discard})
	if err == nil {
		t.Fatal("expected an error for an unresolvable default_model")
	}
	msg := err.Error()
	for _, want := range []string{`"mimo"`, "deepseek-flash", "[[providers]]"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("error %q should mention %q", msg, want)
		}
	}
}

// TestBuildNoticesMissingAPIKey: a resolvable model whose API key env is unset
// builds fine (RequireKey is false so the UI stays reachable) but must emit a
// notice naming the env var, instead of silently showing a dead/empty model.
func TestBuildNoticesMissingAPIKey(t *testing.T) {
	const keyEnv = "REASONIX_MISSING_KEY_FOR_TEST"
	dir := robustTempDir(t)
	t.Chdir(dir)
	writeFile(t, dir, "reasonix.toml", `
default_model = "x"

[codegraph]
enabled = false

[[providers]]
name = "x"
kind = "openai"
base_url = "https://example.invalid"
model = "m"
api_key_env = "`+keyEnv+`"
`)

	var notices []string
	ctrl, err := Build(context.Background(), Options{
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.Notice {
				notices = append(notices, e.Text)
			}
		}),
	})
	if err != nil {
		t.Fatalf("Build should succeed with RequireKey=false even without a key: %v", err)
	}
	defer ctrl.Close()

	found := false
	for _, n := range notices {
		if strings.Contains(n, keyEnv) {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a notice naming the unset key env %q; got %v", keyEnv, notices)
	}
}
