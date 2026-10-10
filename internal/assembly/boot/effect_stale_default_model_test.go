package boot

import (
	"bytes"
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/surface"
	"reasonix/internal/runtime/agent/testutil"
)

const staleDefaultUserConfig = `
default_model = "deepseek-v4-flash"

[[providers]]
name = "deepseek"
kind = "boot-token-profile-test"
model = "deepseek-flash"
`

func staleDefaultNotices(notices *[]event.Event) event.Sink {
	return event.FuncSink(func(e event.Event) {
		if e.Kind == event.Notice && e.Code == event.NoticeCodeDefaultModelUnavailable {
			*notices = append(*notices, e)
		}
	})
}

// A window opens on the first configured model when default_model names
// nothing the [[providers]] list serves, says so with a typed notice, and
// leaves the config file exactly as the user wrote it.
func TestEffectStaleDefaultModelOpensOnAFallback(t *testing.T) {
	isolateConfigHome(t)
	t.Chdir(robustTempDir(t))
	writeUserConfig(t, staleDefaultUserConfig)
	before, err := os.ReadFile(config.UserConfigPath())
	if err != nil {
		t.Fatal(err)
	}
	registerBootTokenProfileTestProvider()
	setBootTokenProfileTestProvider(t, testutil.NewMock("fallback"))

	var notices []event.Event
	res, err := BuildRuntime(context.Background(), Options{
		Sink:                staleDefaultNotices(&notices),
		StatsSource:         surface.Desktop,
		OpenOnFallbackModel: true,
	})
	if err != nil {
		t.Fatalf("BuildRuntime with a stale default_model: %v", err)
	}
	defer res.Controller.Close()
	if got := res.Controller.ModelRef(); got != "deepseek/deepseek-flash" {
		t.Fatalf("model ref = %q, want the first configured model", got)
	}
	if len(notices) != 1 {
		t.Fatalf("default-model notices = %v, want exactly one", notices)
	}
	if d := notices[0].Detail; !strings.Contains(d, `"deepseek-v4-flash"`) || !strings.Contains(d, `"deepseek/deepseek-flash"`) {
		t.Fatalf("notice detail = %q, want both the saved default and the model in use", d)
	}
	after, err := os.ReadFile(config.UserConfigPath())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("config.toml was rewritten:\n--- before\n%s\n--- after\n%s", before, after)
	}
}

// Only a window opens on a fallback. A named model is the caller's choice, and
// an unnamed build that did not ask to fall back (run, the TUI) keeps the
// actionable error rather than moving to another provider.
func TestEffectStaleDefaultModelFailsUnlessAWindowAsks(t *testing.T) {
	for name, opts := range map[string]Options{
		"named":                 {Model: "deepseek-v4-flash", OpenOnFallbackModel: true},
		"unnamed, no fallback":  {},
		"unnamed, desktop only": {StatsSource: surface.Desktop},
	} {
		t.Run(name, func(t *testing.T) {
			isolateConfigHome(t)
			t.Chdir(robustTempDir(t))
			writeUserConfig(t, staleDefaultUserConfig)
			registerBootTokenProfileTestProvider()
			setBootTokenProfileTestProvider(t, testutil.NewMock("explicit"))

			_, err := BuildRuntime(context.Background(), opts)
			if !errors.Is(err, ErrUnknownModel) {
				t.Fatalf("BuildRuntime error = %v, want ErrUnknownModel", err)
			}
		})
	}
}

// A saved default that resolves says nothing, including the legacy bare model
// name the 1.x line writes.
func TestEffectResolvableDefaultModelSaysNothing(t *testing.T) {
	isolateConfigHome(t)
	t.Chdir(robustTempDir(t))
	writeUserConfig(t, strings.Replace(staleDefaultUserConfig, `"deepseek-v4-flash"`, `"deepseek-flash"`, 1))
	registerBootTokenProfileTestProvider()
	setBootTokenProfileTestProvider(t, testutil.NewMock("legacy"))

	var notices []event.Event
	res, err := BuildRuntime(context.Background(), Options{Sink: staleDefaultNotices(&notices), StatsSource: surface.Desktop, OpenOnFallbackModel: true})
	if err != nil {
		t.Fatalf("BuildRuntime: %v", err)
	}
	defer res.Controller.Close()
	if got := res.Controller.ModelRef(); got != "deepseek/deepseek-flash" {
		t.Fatalf("model ref = %q, want deepseek/deepseek-flash", got)
	}
	if len(notices) != 0 {
		t.Fatalf("default-model notices = %v, want none", notices)
	}
}

// With no provider holding a key, the window still opens — on the first
// keyless model, where the missing-key recovery takes over.
func TestEffectStaleDefaultModelWithNoKeyStillOpens(t *testing.T) {
	isolateConfigHome(t)
	t.Chdir(robustTempDir(t))
	writeUserConfig(t, `
default_model = "deepseek-v4-flash"

[[providers]]
name = "deepseek"
kind = "openai"
base_url = "https://api.example.invalid/v1"
api_key_env = "REASONIX_STALE_DEFAULT_TEST_KEY"
models = ["deepseek-flash", "deepseek-pro"]
`)

	var notices []event.Event
	res, err := BuildRuntime(context.Background(), Options{Sink: staleDefaultNotices(&notices), StatsSource: surface.Desktop, OpenOnFallbackModel: true})
	if err != nil {
		t.Fatalf("BuildRuntime with a stale default and no key: %v", err)
	}
	defer res.Controller.Close()
	if got := res.Controller.ModelRef(); got != "deepseek/deepseek-flash" {
		t.Fatalf("model ref = %q, want deepseek/deepseek-flash", got)
	}
	if len(notices) != 1 {
		t.Fatalf("default-model notices = %v, want exactly one", notices)
	}
}

// The shape a shared 1.x/Studio config reaches: the official entry's list has
// moved past the retired flash name, while default_model still names it bare.
func TestEffectRetiredBareDeepSeekDefaultOpensOnFlash(t *testing.T) {
	isolateConfigHome(t)
	t.Chdir(robustTempDir(t))
	writeUserConfig(t, `config_version = 1
default_model = "deepseek-v4-flash"

[[providers]]
name        = "deepseek"
kind        = "anthropic"
base_url    = "https://api.deepseek.com/anthropic"
models      = ["deepseek-v4-flash", "deepseek-v4-pro"]
api_key_env = "DEEPSEEK_API_KEY"
`)
	before, err := os.ReadFile(config.UserConfigPath())
	if err != nil {
		t.Fatal(err)
	}

	var notices []event.Event
	res, err := BuildRuntime(context.Background(), Options{Sink: staleDefaultNotices(&notices), StatsSource: surface.Desktop, OpenOnFallbackModel: true})
	if err != nil {
		t.Fatalf("BuildRuntime with a retired bare default: %v", err)
	}
	defer res.Controller.Close()
	if got := res.Controller.ModelRef(); got != "deepseek/deepseek-flash" {
		t.Fatalf("model ref = %q, want deepseek/deepseek-flash", got)
	}
	if len(notices) != 0 {
		t.Fatalf("default-model notices = %v, want none: the retired name has a successor", notices)
	}
	after, err := os.ReadFile(config.UserConfigPath())
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatalf("config.toml was rewritten:\n%s", after)
	}
}

// A saved default that names a decision source is not a stale name: the source
// exists but answers system_one only. The window opens on the first
// conversation source and the notice says which of the two it is.
func TestEffectDecisionSourceDefaultOpensOnAConversationModel(t *testing.T) {
	isolateConfigHome(t)
	t.Chdir(robustTempDir(t))
	writeUserConfig(t, `
default_model = "laya/typed-decisions"

[[providers]]
name = "laya"
kind = "typesafe"
base_url = "http://127.0.0.1:8700"
models = ["typed-decisions"]

[[providers]]
name = "deepseek"
kind = "boot-token-profile-test"
model = "deepseek-flash"
`)
	registerBootTokenProfileTestProvider()
	setBootTokenProfileTestProvider(t, testutil.NewMock("fallback"))

	var notices []event.Event
	res, err := BuildRuntime(context.Background(), Options{Sink: staleDefaultNotices(&notices), StatsSource: surface.Desktop, OpenOnFallbackModel: true})
	if err != nil {
		t.Fatalf("BuildRuntime with a decision source as default: %v", err)
	}
	defer res.Controller.Close()
	if got := res.Controller.ModelRef(); got != "deepseek/deepseek-flash" {
		t.Fatalf("model ref = %q, want the first conversation model", got)
	}
	if len(notices) != 1 {
		t.Fatalf("default-model notices = %v, want exactly one", notices)
	}
	d := notices[0].Detail
	if !strings.Contains(d, "decision source") || strings.Contains(d, "names no configured") || !strings.Contains(d, `"deepseek/deepseek-flash"`) {
		t.Fatalf("notice detail = %q, want the decision-source reason and the model in use", d)
	}
}
