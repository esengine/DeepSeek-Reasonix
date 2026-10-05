package boot

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
)

// openAIPresetSession assembles the real stack for the curated OpenAI entry as
// an install holds it, on the given wire; both request overrides point at the
// recorder so whichever wire boot picks is the one that gets recorded.
func openAIPresetSession(t *testing.T, wire *modeWire, kind string) error {
	t.Helper()
	preset, ok := config.CuratedProviderPreset("openai")
	if !ok || len(preset.Entries) != 1 {
		t.Fatalf("openai preset = %+v, %v", preset, ok)
	}
	e := preset.Entries[0]
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	t.Setenv(e.APIKeyEnv, "sk-test")
	srv := wire.serve(t)
	writeFile(t, dir, "reasonix.toml", fmt.Sprintf(`
default_model = %q

[agent]
system_prompt = "BASE"

[[providers]]
name = %q
kind = %q
base_url = %q
request_url = %q
chat_url = %q
models = %s
vision_models = %s
default = %q
context_window = %d
supported_efforts = %s
default_effort = %q
preset_id = %q
preset_version = %d
responses_mode = "stateless"
api_key_env = %q
`, e.Name, e.Name, kind, e.BaseURL, srv.URL, srv.URL, tomlList(e.Models), tomlList(e.VisionModels),
		e.Default, e.ContextWindow, tomlList(e.SupportedEfforts), e.DefaultEffort, e.PresetID, e.PresetVersion, e.APIKeyEnv))
	approveWorkspace(t, dir)
	ctrl, err := Build(context.Background(), Options{Sink: event.Discard})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	t.Cleanup(ctrl.Close)
	return ctrl.Run(context.Background(), "reply ok")
}

func tomlList(items []string) string {
	quoted := make([]string, len(items))
	for i, item := range items {
		quoted[i] = strconv.Quote(item)
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}

// assertGPT6ToolTurnOnResponses holds the one combination GPT-6 calls tools on:
// the Responses wire, tools attached, and the preset's effort as reasoning.effort.
func assertGPT6ToolTurnOnResponses(t *testing.T, wire *modeWire, runErr error) {
	t.Helper()
	bodies := wire.loopBodies()
	if len(bodies) == 0 {
		t.Fatal("no recorded request carried tools")
	}
	body := bodies[0]
	if _, chat := body["messages"]; chat {
		t.Fatal("the turn went out on Chat Completions, where GPT-6 calls tools only at reasoning_effort=none")
	}
	if _, ok := body["input"]; !ok {
		t.Fatalf("request is not a Responses body: keys %v", mapKeys(body))
	}
	if tools, _ := body["tools"].([]any); len(tools) == 0 {
		t.Fatal("the loop request carried no tools")
	}
	if body["model"] != "gpt-6-sol" {
		t.Fatalf("model = %v, want the preset default gpt-6-sol", body["model"])
	}
	reasoning, _ := body["reasoning"].(map[string]any)
	if reasoning["effort"] != "medium" {
		t.Fatalf("reasoning = %v, want effort medium", body["reasoning"])
	}
	if _, flat := body["reasoning_effort"]; flat {
		t.Fatal("the Chat Completions reasoning_effort field reached a Responses body")
	}
	if runErr != nil {
		t.Fatalf("Run: %v", runErr)
	}
}

func mapKeys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// A fresh OpenAI preset install reaches the provider on the Responses wire.
func TestEffectOpenAIPresetSendsToolTurnsOnResponses(t *testing.T) {
	preset, _ := config.CuratedProviderPreset("openai")
	wire := &modeWire{}
	assertGPT6ToolTurnOnResponses(t, wire, openAIPresetSession(t, wire, preset.Entries[0].Kind))
}

// An install still holding the Chat Completions shape the preset first shipped
// is moved onto Responses by the load itself, with nothing for its user to do.
func TestEffectShippedChatCompletionsOpenAIPresetMovesToResponses(t *testing.T) {
	wire := &modeWire{}
	assertGPT6ToolTurnOnResponses(t, wire, openAIPresetSession(t, wire, "openai"))
}
