package agent

import (
	"context"
	"encoding/json"
	"reasonix/internal/state/sessionstore"
	"slices"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
)

type estimationProbe struct{ name string }

func (p estimationProbe) Name() string            { return p.name }
func (p estimationProbe) Description() string     { return p.name + " occupies schema tokens" }
func (p estimationProbe) ReadOnly() bool          { return true }
func (p estimationProbe) Schema() json.RawMessage { return json.RawMessage(`{"type":"object"}`) }
func (p estimationProbe) Execute(context.Context, json.RawMessage) (string, error) {
	return "", nil
}

type gatedProbe struct{ estimationProbe }

func (gatedProbe) ProviderVisible(context.Context) bool { return false }

func (gatedProbe) Unavailable(context.Context) tool.Refusal {
	return tool.Refusal{Code: "probe.closed", Message: "closed"}
}

// A token estimate is compared against a window the provider enforces, so it
// has to measure the request that went out. The surface narrowed to what a turn
// admits while the estimate still read the whole registry, which overstates by
// every contextual tool the turn left behind and folds earlier than it needs to.
func TestEstimateMeasuresTheSurfaceTheRequestCarried(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(estimationProbe{name: "always_here"})
	reg.Add(gatedProbe{estimationProbe{name: "never_admitted"}})
	reg.SetProviderVisibleTools([]string{"always_here", "never_admitted"})

	prov := &scriptedProvider{name: "estimation", turns: [][]provider.Chunk{
		{{Type: provider.ChunkText, Text: "done"}, {Type: provider.ChunkDone}},
	}}
	a := New(prov, reg, sessionstore.NewSession("sys"), Options{}, event.Discard)
	if err := a.Run(context.Background(), "go"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(prov.requests) == 0 {
		t.Fatal("provider received no request")
	}
	sent := toolSchemaNames(prov.requests[0].Tools)
	if slices.Contains(sent, "never_admitted") {
		t.Fatalf("the gated tool reached the request, so this arm measures nothing: %v", sent)
	}

	estimated := toolSchemaNames(a.window().estimationSurface())
	if !slices.Equal(estimated, sent) {
		t.Fatalf("the estimate measures a surface no request carried\nestimated %v\nsent      %v", estimated, sent)
	}
}

// Before a request has gone out there is nothing to have carried, and an
// estimate still has to answer. The whole visible set is the answer that errs
// toward folding early rather than overrunning the window.
func TestEstimateFallsBackToTheWholeSurfaceBeforeAnyRequest(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(estimationProbe{name: "always_here"})
	reg.SetProviderVisibleTools([]string{"always_here"})
	a := New(&scriptedProvider{name: "estimation-cold"}, reg, sessionstore.NewSession("sys"), Options{}, event.Discard)
	if got := toolSchemaNames(a.window().estimationSurface()); !slices.Equal(got, []string{"always_here"}) {
		t.Fatalf("cold estimate = %v, want the whole visible surface", got)
	}
}
