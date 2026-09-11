package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

// readProbe is a read-only tool taking a path, so its receipt is classified the
// way a real reader's is — Read=true with Paths set — which is what the progress
// tracker scores as new evidence.
type readProbe struct{}

func (readProbe) Name() string        { return "read_file" }
func (readProbe) Description() string { return "read a file" }
func (readProbe) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"]}`)
}
func (readProbe) ReadOnly() bool { return true }
func (readProbe) Execute(context.Context, json.RawMessage) (string, error) {
	return "package main\n\nfunc main() {}\n", nil
}

// wanderingProvider replays the reported runaway: every round restates the same
// intent and reads one more file. Nothing fails, so the storm breaker stays
// quiet; nothing repeats, so every round scores as new evidence. sameArg swaps
// in one fixed path to isolate novelty as the discriminator.
type wanderingProvider struct {
	rounds  atomic.Int32
	max     int32
	sameArg bool
}

func (p *wanderingProvider) Name() string { return "wandering" }

func (p *wanderingProvider) Stream(_ context.Context, _ provider.Request) (<-chan provider.Chunk, error) {
	round := p.rounds.Add(1)
	ch := make(chan provider.Chunk, 4)
	if round > p.max {
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "Done."}
		ch <- provider.Chunk{Type: provider.ChunkDone}
		close(ch)
		return ch, nil
	}
	path := fmt.Sprintf("internal/pkg%d/file.go", round)
	if p.sameArg {
		path = "HANDOVER.md"
	}
	ch <- provider.Chunk{Type: provider.ChunkText, Text: "收到。先收集当前真实状态，然后彻底重写 HANDOVER.md。"}
	ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{
		ID:        fmt.Sprintf("call-%d", round),
		Name:      "read_file",
		Arguments: fmt.Sprintf(`{"path":%q}`, path),
	}}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

func runRunaway(t *testing.T, sameArg bool, maxRounds int32) (guardFired bool, rounds int32) {
	t.Helper()
	var fired, maintained atomic.Bool
	sink := event.FuncSink(func(e event.Event) {
		if e.Kind == event.Notice && e.Code == event.NoticeCodeProgressGuard {
			fired.Store(true)
		}
		if e.Kind == event.ContextMaintenanceEvent {
			maintained.Store(true)
		}
	})
	t.Cleanup(func() {
		// The repro must not lean on context maintenance: this shape runs away
		// on its own, so a fix to trimming alone cannot bound it.
		if maintained.Load() {
			t.Error("context maintenance ran; the repro is no longer isolated from trimming")
		}
	})
	reg := tool.NewRegistry()
	reg.Add(readProbe{})
	prov := &wanderingProvider{max: maxRounds, sameArg: sameArg}
	// MaxSteps 0 is the shipped default, not a test convenience: [agent].max_steps
	// was retired and is normalized to zero on load.
	a := New(prov, reg, NewSession("sys"), Options{MaxSteps: 0}, sink)

	if err := a.Run(context.Background(), "collect the real state, then rewrite HANDOVER.md"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	return fired.Load(), prov.rounds.Load()
}

// The reported runaway: a read-only turn restating one intent while touching a
// new path each round. It used to score positive gain forever, so the ladder
// could never climb; reading is progress only while it leads somewhere.
func TestReadOnlyWanderingTripsTheProgressGuard(t *testing.T) {
	guardFired, ran := runRunaway(t, false, 40)
	if !guardFired {
		t.Fatalf("wandering for %d rounds never reached the no-progress ladder", ran)
	}
}

// The contrast that names the discriminator. The identical loop reading one
// fixed path is caught within a few rounds: novelty, not usefulness, is what
// the guard measures — and it is the whole difference between a runaway the
// host stops early and one that burns four hours.
func TestRepeatedReadTripsTheProgressGuard(t *testing.T) {
	guardFired, ran := runRunaway(t, true, 40)
	if !guardFired {
		t.Fatalf("repeating one read for %d rounds never fired the guard", ran)
	}
}
