package boot

import (
	"context"
	"strings"
	"testing"

	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/event"
)

func TestBuildOfficialDeepSeekV4ProPrependsPersona(t *testing.T) {
	ctrl := buildOfficialDeepSeekModel(t, "deepseek-v4-pro")
	defer ctrl.Close()

	sys := systemMessage(ctrl.History())
	wantPrefix := config.OfficialDeepSeekV4ProPersona + "\n\n"
	if !strings.HasPrefix(sys, wantPrefix) {
		t.Fatalf("official V4 Pro system prompt must start with the DSH persona:\n%s", sys)
	}
	if !strings.Contains(sys, "BASE") {
		t.Fatalf("persona must keep the configured system prompt, got:\n%s", sys)
	}

	composed := ctrl.Compose("解释 AuthHandler 的 panic")
	if strings.Contains(composed, "简体中文") {
		t.Fatalf("official V4 Pro auto mode must not inject Chinese thinking: %q", composed)
	}
	if !strings.Contains(composed, "use English") {
		t.Fatalf("official V4 Pro auto mode should pin English thinking, got %q", composed)
	}
}

func TestBuildOfficialDeepSeekV4FlashDoesNotPrependPersona(t *testing.T) {
	ctrl := buildOfficialDeepSeekModel(t, "deepseek-v4-flash")
	defer ctrl.Close()

	sys := systemMessage(ctrl.History())
	if strings.Contains(sys, config.OfficialDeepSeekV4ProPersona) {
		t.Fatalf("official V4 Flash must not load the Pro persona:\n%s", sys)
	}
	composed := ctrl.Compose("解释 AuthHandler 的 panic")
	if !strings.Contains(composed, "简体中文") {
		t.Fatalf("flash auto mode should still infer Chinese thinking, got %q", composed)
	}
}

func TestBuildThirdPartyDeepSeekV4ProDoesNotPrependPersona(t *testing.T) {
	isolateConfigHome(t)
	const envName = "BOOT_THIRD_PARTY_V4_PRO_KEY"
	if _, err := config.SetCredential(envName, "test-key"); err != nil {
		t.Fatalf("store credential: %v", err)
	}
	dir := robustTempDir(t)
	t.Chdir(dir)
	writeFile(t, dir, "reasonix.toml", `
default_model = "novita/deepseek-v4-pro"

[agent]
system_prompt = "BASE"

[[providers]]
name = "novita"
kind = "openai"
base_url = "https://api.novita.ai/openai"
model = "deepseek-v4-pro"
api_key_env = "`+envName+`"
`)

	ctrl, err := Build(context.Background(), Options{Sink: event.Discard})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()

	sys := systemMessage(ctrl.History())
	if strings.Contains(sys, config.OfficialDeepSeekV4ProPersona) {
		t.Fatalf("third-party V4 Pro must not load the official persona:\n%s", sys)
	}
}

func buildOfficialDeepSeekModel(t *testing.T, model string) *control.Controller {
	t.Helper()
	isolateConfigHome(t)
	const envName = "BOOT_OFFICIAL_V4_PRO_PERSONA_KEY"
	if _, err := config.SetCredential(envName, "test-key"); err != nil {
		t.Fatalf("store credential: %v", err)
	}
	dir := robustTempDir(t)
	t.Chdir(dir)
	writeFile(t, dir, "reasonix.toml", `
default_model = "deepseek/`+model+`"

[agent]
system_prompt = "BASE"

[[providers]]
name = "deepseek"
kind = "openai"
base_url = "https://api.deepseek.com"
models = ["deepseek-v4-flash", "deepseek-v4-pro"]
default = "deepseek-v4-flash"
api_key_env = "`+envName+`"
`)
	ctrl, err := Build(context.Background(), Options{Sink: event.Discard})
	if err != nil {
		t.Fatalf("Build %s: %v", model, err)
	}
	return ctrl
}
