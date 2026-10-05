package delegation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/runtime/writeclaim"
	"strings"
	"sync"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
)

func TestClassifyFleetFailureReadsIdentityNeverText(t *testing.T) {
	overflow := &provider.APIError{
		Provider: "p", Status: 400,
		Body: `{"error":{"code":"context_length_exceeded","message":"too long"}}`,
	}
	cases := []struct {
		name string
		err  error
		want fleetFailureClass
	}{
		{"nil", nil, fleetFailureUnclassified},
		{"rate limited", &provider.APIError{Provider: "p", Status: 429}, fleetFailureProviderTransient},
		{"server error", &provider.APIError{Provider: "p", Status: 503}, fleetFailureProviderTransient},
		{"request timeout", &provider.APIError{Provider: "p", Status: 408}, fleetFailureProviderTransient},
		{"bad request", &provider.APIError{Provider: "p", Status: 400}, fleetFailureProviderRejected},
		{"unauthorized", &provider.APIError{Provider: "p", Status: 401}, fleetFailureProviderRejected},
		{"unprocessable", &provider.APIError{Provider: "p", Status: 422}, fleetFailureProviderRejected},
		{"context overflow beats the plain 4xx answer", overflow, fleetFailureContextExhausted},
		{"compaction required", fmt.Errorf("child: %w", agent.ErrCompactionRequired), fleetFailureContextExhausted},
		{"stream interrupted", &provider.StreamInterruptedError{Err: errors.New("eof")}, fleetFailureProviderTransient},
		{"wrapped api error", fmt.Errorf("sub-agent: %w", &provider.APIError{Provider: "p", Status: 429}), fleetFailureProviderTransient},
		// The guard that matters: an error whose producer named nothing stays
		// unclassified, however much its message sounds like a known class.
		{"message says rate limit", errors.New("rate limit exceeded, please retry in 429 seconds"), fleetFailureUnclassified},
		{"message says context length", errors.New("context_length_exceeded"), fleetFailureUnclassified},
		{"plain failure", errors.New("scripted provider failure"), fleetFailureUnclassified},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyFleetFailure(tc.err); got != tc.want {
				t.Fatalf("classifyFleetFailure(%v) = %q, want %q", tc.err, got, tc.want)
			}
		})
	}
}

func TestFleetStatusLineOmitsAnUnclassifiedReason(t *testing.T) {
	if got := fleetStatusLine("failed", fleetFailureUnclassified); got != "status: failed\n" {
		t.Fatalf("unclassified status line = %q", got)
	}
	want := "status: failed reason=provider.transient\n"
	if got := fleetStatusLine("failed", fleetFailureProviderTransient); got != want {
		t.Fatalf("classified status line = %q, want %q", got, want)
	}
}

// fleetAPIErrorProvider fails the item whose prompt contains "FAIL" with a real
// provider identity, and completes every other item.
type fleetAPIErrorProvider struct {
	status int
	mu     sync.Mutex
	ran    map[string]bool
}

func (p *fleetAPIErrorProvider) Name() string { return "fleet-api-error" }

func (p *fleetAPIErrorProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	last := ""
	for _, m := range req.Messages {
		if m.Role == provider.RoleUser {
			last = m.Content
		}
	}
	p.mu.Lock()
	if p.ran == nil {
		p.ran = map[string]bool{}
	}
	for _, name := range []string{"downstream", "sibling"} {
		if strings.Contains(last, name) {
			p.ran[name] = true
		}
	}
	p.mu.Unlock()
	if strings.Contains(last, "FAIL") {
		return nil, &provider.APIError{Provider: "fleet-api-error", Status: p.status}
	}
	ch := make(chan provider.Chunk, 2)
	ch <- provider.Chunk{Type: provider.ChunkText, Text: "done"}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

func (p *fleetAPIErrorProvider) started(name string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.ran[name]
}

// TestFleetAggregateCarriesTheFailureIdentity is the effect test for this
// change: the code has to reach the caller through the real aggregate, on the
// failed item and on the branch its failure killed.
func TestFleetAggregateCarriesTheFailureIdentity(t *testing.T) {
	root := testenv.TempDir(t)
	prov := &fleetAPIErrorProvider{status: 429}
	reg := tool.NewRegistry()
	reg.Add(fakeReadFileTool{})
	task := NewTaskTool(prov, nil, reg, 20, 0, 0, 0, 0.0, "", "sys", nil, 0, "", "", nil).
		WithTranscripts(mustSubagentStore(t), root, "base", "high").
		WithScheduler(writeclaim.NewSubagentScheduler(3, 3))
	fleet := NewFleetTool(task)
	ctx := agent.WithCallContext(context.Background(), "fleet-call", event.Discard, nil, false)
	ctx = agent.WithParentSession(ctx, "parent-session")
	args := json.RawMessage(`{"tasks":[
		{"id":"head","prompt":"FAIL here","read_only":true},
		{"id":"downstream","prompt":"downstream work","depends_on":["head"],"read_only":true},
		{"id":"sibling","prompt":"sibling work","read_only":true}
	]}`)
	out, err := fleet.Execute(ctx, args)
	// A failed branch is not an interruption: the run reached its end and
	// reported, so the caller must not receive context.Canceled for it.
	if err != nil {
		t.Fatalf("a fleet that ran to its end must not answer an error: %v", err)
	}
	if !strings.Contains(out, "Fleet finished, 1 of 3 tasks answered") {
		t.Fatalf("aggregate headline must say what actually happened:\n%s", out)
	}
	if !strings.Contains(out, "status: failed reason=provider.transient") {
		t.Fatalf("failed item did not carry its identity:\n%s", out)
	}
	if !strings.Contains(out, "status: skipped reason=provider.transient") {
		t.Fatalf("skipped branch did not inherit the identity:\n%s", out)
	}
	if prov.started("downstream") {
		t.Fatal("downstream ran despite its dependency failing")
	}
	if !prov.started("sibling") {
		t.Fatal("independent sibling was skipped without fail_fast")
	}
}

// TestFleetCancellationStillAnswersCanceled is the other half of the same
// boundary: a real interruption must keep answering context.Canceled, which is
// what the turn loop reads to tell it from a run that finished and reported.
func TestFleetCancellationStillAnswersCanceled(t *testing.T) {
	root := testenv.TempDir(t)
	prov := &fleetAPIErrorProvider{status: 429}
	reg := tool.NewRegistry()
	reg.Add(fakeReadFileTool{})
	task := NewTaskTool(prov, nil, reg, 20, 0, 0, 0, 0.0, "", "sys", nil, 0, "", "", nil).
		WithTranscripts(mustSubagentStore(t), root, "base", "high").
		WithScheduler(writeclaim.NewSubagentScheduler(2, 2))
	fleet := NewFleetTool(task)
	ctx, cancel := context.WithCancel(agent.WithCallContext(context.Background(), "fleet-call", event.Discard, nil, false))
	cancel()
	out, err := fleet.Execute(ctx, json.RawMessage(`{"tasks":[
		{"id":"one","prompt":"one","read_only":true},
		{"id":"two","prompt":"two","read_only":true}
	]}`))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled fleet error = %v, want context.Canceled", err)
	}
	if !strings.Contains(out, "Cancelled fleet after completing 0 of 2 tasks") {
		t.Fatalf("cancelled aggregate headline = %s", out)
	}
}

// TestFleetAggregateLeavesAnUnnamedFailureUnclassified keeps the honest half of
// the contract visible: a provider error with no identity is reported without a
// reason rather than given a guessed one.
func TestFleetAggregateLeavesAnUnnamedFailureUnclassified(t *testing.T) {
	root := testenv.TempDir(t)
	prov := &fleetScriptedFailureProvider{}
	reg := tool.NewRegistry()
	reg.Add(fakeReadFileTool{})
	task := NewTaskTool(prov, nil, reg, 20, 0, 0, 0, 0.0, "", "sys", nil, 0, "", "", nil).
		WithTranscripts(mustSubagentStore(t), root, "base", "high").
		WithScheduler(writeclaim.NewSubagentScheduler(2, 2))
	fleet := NewFleetTool(task)
	ctx := agent.WithCallContext(context.Background(), "fleet-call", event.Discard, nil, false)
	ctx = agent.WithParentSession(ctx, "parent-session")
	args := json.RawMessage(`{"tasks":[
		{"id":"head","prompt":"FAIL here","read_only":true},
		{"id":"implement","prompt":"implement work","depends_on":["head"],"read_only":true}
	]}`)
	out, err := fleet.Execute(ctx, args)
	if err != nil {
		t.Fatalf("a fleet that ran to its end must not answer an error: %v", err)
	}
	if !strings.Contains(out, "status: failed\n") {
		t.Fatalf("unnamed failure should carry no reason:\n%s", out)
	}
	if strings.Contains(out, "reason=") {
		t.Fatalf("unnamed failure was given a guessed identity:\n%s", out)
	}
}
