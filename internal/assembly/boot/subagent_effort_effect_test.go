package boot

// Effect tests assert inherited effort at the real provider boundary: Build
// assembles the parent, the parent dispatches task:subagent, and the child is
// recorded with the provider configuration and request it actually receives.

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
)

type inheritedEffortProviderCall struct {
	model         string
	defaultEffort string
	request       provider.Request
}

type inheritedEffortEffectProvider struct {
	mu    sync.Mutex
	calls []inheritedEffortProviderCall
}

type inheritedEffortEffectProviderInstance struct {
	model         string
	owner         *inheritedEffortEffectProvider
	defaultEffort string
}

func (p *inheritedEffortEffectProviderInstance) Name() string {
	return "boot-inherited-effort"
}

func (p *inheritedEffortEffectProviderInstance) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.owner.mu.Lock()
	call := len(p.owner.calls)
	p.owner.calls = append(p.owner.calls, inheritedEffortProviderCall{
		model:         p.model,
		defaultEffort: p.defaultEffort,
		request:       req,
	})
	p.owner.mu.Unlock()

	ch := make(chan provider.Chunk, 2)
	if call == 0 {
		ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{
			ID:        "subagent-1",
			Name:      "use_capability",
			Arguments: `{"action":"call","capability_id":"task:subagent","arguments":{"description":"effort probe","prompt":"reply done and stop"}}`,
		}}
	} else {
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "done"}
	}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

func (p *inheritedEffortEffectProvider) callsSnapshot() []inheritedEffortProviderCall {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]inheritedEffortProviderCall(nil), p.calls...)
}

func runInheritedEffortEffect(t *testing.T, supported bool) ([]inheritedEffortProviderCall, []event.Event, *config.Config) {
	t.Helper()
	return runInheritedEffortEffectWith(t, supported, "", "")
}

func runInheritedEffortEffectWith(t *testing.T, supported bool, agentExtra, providerExtra string) ([]inheritedEffortProviderCall, []event.Event, *config.Config) {
	t.Helper()
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)

	providerKind := fmt.Sprintf("boot-inherited-effort-%t-%s", supported, strings.ReplaceAll(t.Name(), "/", "-"))
	recorder := &inheritedEffortEffectProvider{}
	provider.Register(providerKind, func(cfg provider.Config) (provider.Provider, error) {
		defaultEffort, _ := cfg.Extra["effort"].(string)
		return &inheritedEffortEffectProviderInstance{owner: recorder, model: cfg.Model, defaultEffort: defaultEffort}, nil
	})

	supportedEfforts := `["low", "high"]`
	if supported {
		supportedEfforts = `["low", "high", "max"]`
	}
	writeFile(t, dir, "reasonix.toml", fmt.Sprintf(`
default_model = "test-model"

[codegraph]
enabled = false

[agent]
system_prompt = "BASE"
subagent_effort = "max"
%s

[[providers]]
name = "test-model"
kind = %q
model = "x"
supported_efforts = %s
default_effort = "high"
%s
`, agentExtra, providerKind, supportedEfforts, strings.ReplaceAll(providerExtra, "%KIND%", providerKind)))
	approveWorkspace(t, dir)

	var sinkMu sync.Mutex
	var events []event.Event
	sink := event.FuncSink(func(e event.Event) {
		sinkMu.Lock()
		events = append(events, e)
		sinkMu.Unlock()
	})

	ctrl, err := Build(context.Background(), Options{Sink: sink})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()
	if err := ctrl.Run(context.Background(), "delegate one task"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	cfg, err := config.LoadForRootReadOnly(dir)
	if err != nil {
		t.Fatalf("LoadForRootReadOnly: %v", err)
	}
	sinkMu.Lock()
	events = append([]event.Event(nil), events...)
	sinkMu.Unlock()
	return recorder.callsSnapshot(), events, cfg
}

func TestEffectUnsupportedInheritedSubagentEffortFallsBack(t *testing.T) {
	calls, events, cfg := runInheritedEffortEffect(t, false)
	if len(calls) < 3 {
		t.Fatalf("provider calls = %d, want parent, subagent, and parent follow-up", len(calls))
	}
	child := calls[1]
	if child.defaultEffort != "high" || child.request.EffortOverride != "" {
		t.Fatalf("child provider effort = default %q/override %q, want provider default high without inherited override", child.defaultEffort, child.request.EffortOverride)
	}
	if cfg.Agent.SubagentEffort != "max" {
		t.Fatalf("persisted agent.subagent_effort = %q, want max", cfg.Agent.SubagentEffort)
	}

	for _, e := range events {
		if e.Kind == event.Notice && e.Level == event.LevelWarn &&
			strings.Contains(e.Detail, "agent.subagent_effort") &&
			strings.Contains(e.Detail, "provider/model default") {
			return
		}
	}
	t.Fatalf("warning for dropped inherited effort not delivered: %+v", events)
}

func TestEffectSupportedInheritedSubagentEffortReachesSubagent(t *testing.T) {
	calls, events, cfg := runInheritedEffortEffect(t, true)
	if len(calls) < 3 {
		t.Fatalf("provider calls = %d, want parent, subagent, and parent follow-up", len(calls))
	}
	child := calls[1]
	if child.defaultEffort != "max" {
		t.Fatalf("child provider default effort = %q, want max", child.defaultEffort)
	}
	if child.request.EffortOverride != "" && child.request.EffortOverride != "max" {
		t.Fatalf("child request effort override = %q, want empty or max", child.request.EffortOverride)
	}
	if cfg.Agent.SubagentEffort != "max" {
		t.Fatalf("persisted agent.subagent_effort = %q, want max", cfg.Agent.SubagentEffort)
	}
	for _, e := range events {
		if e.Kind == event.Notice && strings.Contains(e.Detail, "agent.subagent_effort") {
			t.Fatalf("supported inherited effort emitted a dropped warning: %+v", e)
		}
	}
}

func TestEffectDroppedInheritedEffortFallsToParentEffort(t *testing.T) {
	calls, events, cfg := runInheritedEffortEffectWith(t, false, "", `effort = "low"`)
	if len(calls) < 3 {
		t.Fatalf("provider calls = %d, want parent, subagent, and parent follow-up", len(calls))
	}
	if got := calls[1].defaultEffort; got != "low" {
		t.Fatalf("child provider effort = %q, want the parent's configured low", got)
	}
	if cfg.Agent.SubagentEffort != "max" {
		t.Fatalf("persisted agent.subagent_effort = %q, want max", cfg.Agent.SubagentEffort)
	}
	warned := false
	for _, e := range events {
		warned = warned || (e.Kind == event.Notice && strings.Contains(e.Detail, "agent.subagent_effort"))
	}
	if !warned {
		t.Fatal("dropping the inherited effort should still be announced")
	}
}

const otherModelProvider = `

[[providers]]
name = "other"
kind = "%KIND%"
model = "y"
supported_efforts = %SUPPORTED%
default_effort = "low"
`

func otherModel(supported string) string {
	return strings.ReplaceAll(otherModelProvider, "%SUPPORTED%", supported)
}

func TestEffectInheritedEffortFollowsTheModelSubagentModelsTaskSelects(t *testing.T) {
	for _, tc := range []struct {
		name      string
		supported string
		want      string
	}{
		{"model without the level runs at its own default", `["low", "high"]`, "low"},
		{"model declaring the level receives it", `["low", "high", "max"]`, "max"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls, events, cfg := runInheritedEffortEffectWith(t, true, `subagent_models = { task = "other/y" }`, otherModel(tc.supported))
			if len(calls) < 3 {
				t.Fatalf("provider calls = %d, want parent, subagent, and parent follow-up", len(calls))
			}
			child := calls[1]
			if child.model != "y" {
				t.Fatalf("child ran model %q, want y: the configured task model", child.model)
			}
			if child.defaultEffort != tc.want || (child.request.EffortOverride != "" && child.request.EffortOverride != tc.want) {
				t.Fatalf("child effort = default %q/override %q, want %q", child.defaultEffort, child.request.EffortOverride, tc.want)
			}
			if cfg.Agent.SubagentEffort != "max" {
				t.Fatalf("persisted agent.subagent_effort = %q, want max", cfg.Agent.SubagentEffort)
			}
			warned := false
			for _, e := range events {
				warned = warned || (e.Kind == event.Notice && strings.Contains(e.Detail, "agent.subagent_effort"))
			}
			if warned != (tc.want != "max") {
				t.Fatalf("dropped-effort warning delivered = %v, want %v", warned, tc.want != "max")
			}
		})
	}
}
