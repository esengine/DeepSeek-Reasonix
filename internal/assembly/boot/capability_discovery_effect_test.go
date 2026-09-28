package boot

// Effect test for capability discovery: what a model actually gets back when
// it checks whether a delegation capability exists before dispatching.

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/runtime/usecap"
)

// capabilityProbeProvider issues one scripted use_capability call per round,
// then finishes. Round i+1's request carries round i's tool result.
type capabilityProbeProvider struct {
	mu    sync.Mutex
	calls []string
	round int
	reqs  []provider.Request
}

func (p *capabilityProbeProvider) Name() string { return "boot-capability-probe" }

func (p *capabilityProbeProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.mu.Lock()
	p.reqs = append(p.reqs, req)
	i := p.round
	p.round++
	p.mu.Unlock()
	ch := make(chan provider.Chunk, 3)
	if i < len(p.calls) {
		ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{
			ID: fmt.Sprintf("probe-%d", i), Name: "use_capability", Arguments: p.calls[i],
		}}
	} else {
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "done"}
	}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

func (p *capabilityProbeProvider) toolResults() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []string
	seen := map[string]bool{}
	for _, req := range p.reqs {
		for _, m := range req.Messages {
			if m.Role != provider.RoleTool || seen[m.Content] {
				continue
			}
			seen[m.Content] = true
			out = append(out, m.Content)
		}
	}
	return out
}

// probeCapabilities runs the real Build stack, letting the model issue the
// given use_capability calls, and returns each call's result text.
func probeCapabilities(t *testing.T, kind string, calls []string) []string {
	t.Helper()
	probe := &capabilityProbeProvider{calls: calls}
	runProbeWith(t, kind, probe, event.Discard)
	results := probe.toolResults()
	if len(results) != len(calls) {
		t.Fatalf("expected %d tool results, got %d: %v", len(calls), len(results), results)
	}
	return results
}

// runProbeWith builds the real stack around the probe and runs one turn,
// reporting events to the given sink.
func runProbeWith(t *testing.T, kind string, probe *capabilityProbeProvider, sink event.Sink) {
	t.Helper()
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)

	provider.Register(kind, func(provider.Config) (provider.Provider, error) { return probe, nil })
	writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"

[agent]
system_prompt = "BASE"

[[providers]]
name = "test-model"
kind = "`+kind+`"
model = "x"
`)
	approveWorkspace(t, dir)
	ctrl, err := Build(context.Background(), Options{Sink: sink})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()
	if err := ctrl.Run(context.Background(), "check what you can delegate to"); err != nil {
		t.Fatalf("Run: %v", err)
	}
}

// TestEffectDescribedCapabilityIDsAreDiscoverable pins the promise at its final
// boundary. The proxy description names concrete ids; a model that verifies one
// before dispatching must not be told it does not exist. `task:subagent` failed
// exactly that way — call accepted it while inspect denied it — so a subagent
// harness looked absent and the work ran inline instead.
func TestEffectDescribedCapabilityIDsAreDiscoverable(t *testing.T) {
	calls := make([]string, 0, len(usecap.CapabilityIDExamples))
	for _, id := range usecap.CapabilityIDExamples {
		calls = append(calls, `{"action":"inspect","capability_id":"`+id+`"}`)
	}
	results := probeCapabilities(t, "boot-capability-probe-inspect", calls)
	for i, id := range usecap.CapabilityIDExamples {
		if strings.Contains(results[i], "unknown capability_id") {
			t.Errorf("the description names %q but inspect denies it exists: %s", id, results[i])
		}
	}
}

func TestEffectCapabilitySearchContractReachesFirstRequest(t *testing.T) {
	probe := &capabilityProbeProvider{}
	runProbeWith(t, "boot-capability-search-contract", probe, event.Discard)
	probe.mu.Lock()
	defer probe.mu.Unlock()
	if len(probe.reqs) == 0 {
		t.Fatal("provider received no request")
	}
	for _, schema := range probe.reqs[0].Tools {
		if schema.Name != "use_capability" {
			continue
		}
		if !strings.Contains(schema.Description, "Before saying a tool, network, or integration is unavailable") ||
			!strings.Contains(string(schema.Parameters), `"query"`) ||
			!strings.Contains(string(schema.Parameters), `"search | list | inspect | call | decline"`) {
			t.Fatalf("first request carries an incomplete capability search contract:\n%s\n%s", schema.Description, schema.Parameters)
		}
		return
	}
	t.Fatal("first provider request is missing use_capability")
}

// TestEffectDelegationIDsCarryTheirSchema closes the second half: an id the
// provider schema hides is callable only if inspect hands back its arguments.
// Description text alone left the model guessing them.
func TestEffectDelegationIDsCarryTheirSchema(t *testing.T) {
	results := probeCapabilities(t, "boot-capability-probe-schema", []string{
		`{"action":"inspect","capability_id":"task:subagent"}`,
		`{"action":"list"}`,
	})
	var inspected struct {
		ID          string          `json:"id"`
		Aliases     []string        `json:"aliases"`
		ToolName    string          `json:"tool_name"`
		InputSchema json.RawMessage `json:"input_schema"`
	}
	body, _, _ := strings.Cut(results[0], "\n\nTools")
	if err := json.Unmarshal([]byte(body), &inspected); err != nil {
		t.Fatalf("inspect result is not the documented JSON shape: %v\n%s", err, results[0])
	}
	if inspected.ToolName != "task" {
		t.Errorf("task:subagent inspects as tool %q, want task", inspected.ToolName)
	}
	if len(inspected.InputSchema) == 0 {
		t.Errorf("no input schema for a dispatcher the provider schema hides:\n%s", results[0])
	}
	if !strings.Contains(string(inspected.InputSchema), "prompt") {
		t.Errorf("task schema names no prompt argument:\n%s", inspected.InputSchema)
	}
	if !strings.Contains(results[1], "task:subagent") {
		t.Errorf("action=list never names task:subagent, so nothing points a model at it:\n%s", results[1])
	}
}
