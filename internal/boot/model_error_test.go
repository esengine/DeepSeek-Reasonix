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
// (e.g. a stale preset name after [[providers]] replaced the built-in presets) must
// fail with a message that names the model, lists what IS configured, and hints
// at the [[providers]] trap — not a silent empty model. This contract holds when
// the project file is the only config, so isolate REASONIX_HOME: a user-global
// config with an explicit default_model would instead rescue the boot (#4218).
func TestBuildUnknownModelErrorIsActionable(t *testing.T) {
	t.Setenv("REASONIX_HOME", t.TempDir())
	dir := robustTempDir(t)
	t.Chdir(dir)
	writeFile(t, dir, "reasonix.toml", `
default_model = "legacy-missing"

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
	for _, want := range []string{`"legacy-missing"`, "deepseek-flash", "[[providers]]"} {
		if !strings.Contains(msg, want) {
			t.Fatalf("error %q should mention %q", msg, want)
		}
	}
}

func TestBuildNoticesProjectDefaultModelFallback(t *testing.T) {
	home := t.TempDir()
	t.Setenv("REASONIX_HOME", home)
	writeFile(t, home, "config.toml", `
default_model = "deepseek-pro"

[[providers]]
name = "deepseek-pro"
kind = "openai"
base_url = "https://example.invalid"
model = "deepseek-v4-pro"
api_key_env = "REASONIX_TEST_KEY_UNSET"
`)

	dir := robustTempDir(t)
	t.Chdir(dir)
	writeFile(t, dir, "reasonix.toml", `
default_model = "deepseek-flash"
`)

	var notices []event.Event
	ctrl, err := Build(context.Background(), Options{
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.Notice {
				notices = append(notices, e)
			}
		}),
	})
	if err != nil {
		t.Fatalf("Build should fall back to the user default model: %v", err)
	}
	defer ctrl.Close()

	for _, notice := range notices {
		if notice.Level == event.LevelWarn &&
			notice.Text == "Ignored the project config's default_model." &&
			strings.Contains(notice.Detail, `default_model = "deepseek-flash"`) &&
			strings.Contains(notice.Detail, `using "deepseek-pro"`) {
			return
		}
	}
	t.Fatalf("expected a warning naming the ignored project model and user fallback; got %v", notices)
}

func TestBuildMigratesLegacyBareMimoModelOverride(t *testing.T) {
	dir := robustTempDir(t)
	t.Chdir(dir)
	writeFile(t, dir, "reasonix.toml", `
default_model = "deepseek-flash"

[[providers]]
name = "deepseek-flash"
kind = "openai"
base_url = "https://example.invalid"
model = "deepseek-v4-flash"
api_key_env = "REASONIX_TEST_KEY_UNSET"
`)

	ctrl, err := Build(context.Background(), Options{Sink: event.Discard, Model: "mimo-v2.5-pro"})
	if err != nil {
		t.Fatalf("Build should migrate legacy bare MiMo model override: %v", err)
	}
	defer ctrl.Close()
	if ctrl.Label() != "mimo-v2.5-pro" {
		t.Fatalf("controller label = %q, want mimo-v2.5-pro", ctrl.Label())
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

[[providers]]
name = "x"
kind = "openai"
base_url = "https://example.invalid"
model = "m"
api_key_env = "`+keyEnv+`"
`)

	var notices []event.Event
	ctrl, err := Build(context.Background(), Options{
		Sink: event.FuncSink(func(e event.Event) {
			if e.Kind == event.Notice {
				notices = append(notices, e)
			}
		}),
	})
	if err != nil {
		t.Fatalf("Build should succeed with RequireKey=false even without a key: %v", err)
	}
	defer ctrl.Close()

	found := false
	for _, n := range notices {
		if n.Text == "Selected model is missing its API key." && strings.Contains(n.Detail, keyEnv) {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected a notice naming the unset key env %q; got %v", keyEnv, notices)
	}
}

func TestBuildDoesNotNoticeMissingAPIKeyForNoAuthLoopback(t *testing.T) {
	const keyEnv = "REASONIX_LOCAL_GATEWAY_KEY_FOR_TEST"
	dir := robustTempDir(t)
	t.Chdir(dir)
	t.Setenv(keyEnv, "")
	writeFile(t, dir, "reasonix.toml", `
default_model = "local/model-a"

[[providers]]
name = "local"
kind = "openai"
base_url = "http://127.0.0.1:23333/v1"
models = ["model-a"]
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
		t.Fatalf("Build should allow no-auth loopback provider without a key: %v", err)
	}
	defer ctrl.Close()

	for _, n := range notices {
		if strings.Contains(n, keyEnv) {
			t.Fatalf("did not expect missing-key notice for loopback no-auth provider; got %v", notices)
		}
	}
}
