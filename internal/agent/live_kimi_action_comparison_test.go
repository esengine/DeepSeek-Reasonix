//go:build live

package agent

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/config"
	"reasonix/internal/event"
	"reasonix/internal/tool"
)

type kimiActionFixture struct {
	path, action, expected string
	proposed, executed     atomic.Int32
}

func (f *kimiActionFixture) Name() string { return f.action + "_fixture" }
func (f *kimiActionFixture) Description() string {
	return "Perform the requested fixture operation. read returns the file contents, edit replaces the file with value, verify runs the fixed fixture verification test. Return real results only."
}
func (f *kimiActionFixture) ReadOnly() bool { return f.action != "edit" }
func (f *kimiActionFixture) Schema() json.RawMessage {
	if f.action == "edit" {
		return json.RawMessage(`{"type":"object","properties":{"value":{"type":"string"}},"required":["value"],"additionalProperties":false}`)
	}
	return json.RawMessage(`{"type":"object","properties":{},"additionalProperties":false}`)
}
func (f *kimiActionFixture) Execute(ctx context.Context, raw json.RawMessage) (string, error) {
	f.proposed.Add(1)
	if f.action == "edit" {
		var v struct {
			Value string `json:"value"`
		}
		if err := json.Unmarshal(raw, &v); err != nil {
			return "", err
		}
		if v.Value != f.expected {
			return "", deterministicApplicationError("value did not match the requested Unicode/newline text; decode the JSON string before passing value. No write occurred.")
		}
		if err := os.WriteFile(f.path, []byte(v.Value), 0600); err != nil {
			return "", err
		}
	} else if f.action == "verify" {
		binary, err := os.Executable()
		if err != nil {
			return "", err
		}
		cmd := exec.CommandContext(ctx, binary, "-test.run=^TestLiveKimiFixedVerifierChild$")
		cmd.Env = []string{"REASONIX_FIXTURE_VERIFY=" + f.path, "REASONIX_FIXTURE_EXPECTED=" + f.expected}
		if output, err := cmd.CombinedOutput(); err != nil {
			return string(output), err
		}
	}
	content, err := os.ReadFile(f.path)
	if err != nil {
		return "", err
	}
	f.executed.Add(1)
	return string(content), nil
}

// The child executes this fixed verifier only, with no provider credentials or
// arbitrary model-controlled commands, paths, or executable source.
func TestLiveKimiFixedVerifierChild(t *testing.T) {
	path := os.Getenv("REASONIX_FIXTURE_VERIFY")
	if path == "" {
		t.Skip("fixed verifier child only")
	}
	actual, err := os.ReadFile(path)
	if err != nil || string(actual) != os.Getenv("REASONIX_FIXTURE_EXPECTED") {
		t.Fatal("fixture verification failed")
	}
}

func TestLiveKimiActionComparison(t *testing.T) {
	if os.Getenv("OPENCODE_GO_API_KEY") == "" {
		t.Skip("Go credential unavailable")
	}
	var tc multiProviderCase
	for _, candidate := range multiProviderCases() {
		if candidate.vendor == "go" && candidate.model == "kimi-k3" && candidate.protocol == "chat" {
			tc = candidate
			break
		}
	}
	for _, effort := range []string{"low", "max"} {
		for _, action := range []string{"read", "edit", "verify"} {
			for sample := 0; sample < 10; sample++ {
				for _, candidate := range []bool{sample%2 == 0, sample%2 != 0} {
					t.Run(fmt.Sprintf("%s/%s/%02d/candidate_%t", effort, action, sample, candidate), func(t *testing.T) {
						t.Parallel()
						caseConfig := tc
						caseConfig.effort = effort
						p := caseConfig.new(t, "", "baseline")
						marker := "fixture-" + rand.Text()
						f := &kimiActionFixture{path: filepath.Join(t.TempDir(), "fixture.txt"), action: action, expected: marker}
						if action == "edit" {
							f.expected = "中文🙂\n" + marker
						}
						if err := os.WriteFile(f.path, []byte(marker), 0600); err != nil {
							t.Fatal(err)
						}
						reg := tool.NewRegistry()
						reg.Add(f)
						system := config.DefaultSystemPrompt + "\n\n" + config.UserDecisionPolicy + "\n\n" + config.WorkPracticePolicy + "\n\n" + config.LanguagePolicy
						if candidate {
							system += "\n\n" + config.KimiActionPolicy
						}
						session := NewSession(system)
						sink := &recordSink{}
						a := New(p, reg, session, Options{MaxSteps: 5, MaxOutputTokens: 2048, MissingReasoningWarnStateDir: t.TempDir()}, sink)
						prompt := "Use " + f.Name() + " to " + action + " the fixture and report the actual returned contents. Do not invent the contents."
						if action == "edit" {
							value, _ := json.Marshal(f.expected)
							prompt = "Edit the fixture to the decoded value of this JSON string: " + string(value) + ". Use edit_fixture and report the actual result. If a call is rejected before writing, correct it; do not repeat a successful write."
						}
						ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
						defer cancel()
						start := time.Now()
						err := a.Run(ctx, prompt)
						requests, tokens, output := 0, 0, 0
						unknown := false
						for _, e := range sink.kinds(event.Usage) {
							if e.Usage != nil {
								requests += e.Usage.RequestCount
								tokens += e.Usage.PromptTokens
								output += e.Usage.CompletionTokens
								unknown = unknown || e.Usage.Unknown
							}
						}
						messages := session.Snapshot()
						proposals, firstValid := 0, false
						for _, message := range messages {
							for _, call := range message.ToolCalls {
								proposals++
								if proposals == 1 {
									var values map[string]any
									valid := json.Unmarshal([]byte(call.Arguments), &values) == nil && values != nil && call.Name == f.Name()
									if action == "edit" {
										valid = valid && len(values) == 1 && values["value"] == f.expected
									} else {
										valid = valid && len(values) == 0
									}
									firstValid = valid
								}
							}
						}
						answer := ""
						if len(messages) > 0 {
							answer = messages[len(messages)-1].Content
						}
						passed := err == nil && f.executed.Load() == 1 && strings.Contains(answer, marker)
						metric := map[string]any{"effort": effort, "action": action, "sample": sample, "candidate": candidate, "passed": passed, "proposed": proposals, "first_arguments_correct": firstValid, "entered_execute": f.proposed.Load(), "executed": f.executed.Load(), "requests": requests, "prompt": tokens, "output": output, "unknown_usage": unknown, "elapsed_ms": time.Since(start).Milliseconds(), "error": fmt.Sprint(err)}
						encoded, _ := json.Marshal(metric)
						t.Logf("METRIC %s", encoded)
						if !passed {
							t.Fail()
						}
					})
				}
			}
		}
	}
}
