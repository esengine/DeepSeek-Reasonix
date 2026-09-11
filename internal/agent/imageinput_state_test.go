package agent

import (
	"context"
	"encoding/json"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/imageinput"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

func TestImageCancellationPreservesRecoveryEvidence(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	gate := &recordingRecoveryGate{}
	cfg := &imageinput.Config{Model: "vision/model", Resolve: func(string) (provider.Provider, error) {
		if !gate.observation.Success || gate.observation.Cancelled {
			t.Error("original recovery receipt missing before image request")
		}
		cancel()
		return nil, ctx.Err()
	}}
	reg := tool.NewRegistry()
	shot := &detailedImageTool{fakeImageTool: fakeImageTool{text: "screenshot saved", images: []string{"data:image/png;base64,QUFB"}}}
	reg.Add(shot)
	p := &scriptedProvider{name: "text", turns: [][]provider.Chunk{{toolCallChunk("c1", "shot", `{}`), {Type: provider.ChunkDone}}}}
	a := New(p, reg, NewSession("sys"), Options{ImageInput: cfg, ModelRef: "text/model", RecoveryGate: gate}, event.Discard)
	_ = a.Run(ctx, "inspect")
	data, err := json.Marshal(a.Session().Snapshot())
	if err != nil {
		t.Fatal(err)
	}
	var restored []provider.Message
	if err = json.Unmarshal(data, &restored); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, m := range restored {
		if m.Role == provider.RoleTool {
			found = true
			var recovery provider.InterruptedTurnRecovery
			provider.RecordToolRecovery(&recovery, provider.InterruptedToolSummary{ID: m.ToolCallID, Name: m.Name}, provider.ToolResultRunState(m))
			if len(recovery.CompletedTools) != 1 || len(recovery.UnknownTools) != 0 {
				t.Fatalf("recovery: %+v", recovery)
			}
		}
	}
	if !found || shot.calls.Load() != 1 {
		t.Fatalf("found=%v calls=%d", found, shot.calls.Load())
	}
}

func TestOutcomeRunStatePreservesOriginalUncertainty(t *testing.T) {
	for _, tc := range []struct {
		out  toolOutcome
		want provider.ToolRunState
	}{
		{toolOutcome{}, provider.ToolRunNotStarted},
		{toolOutcome{executed: true, output: "write outcome unknown:"}, provider.ToolRunUnknown},
		{toolOutcome{executed: true, errMsg: "context canceled"}, provider.ToolRunUnknown},
		{toolOutcome{executed: true, errMsg: "invalid format"}, provider.ToolRunCompleted},
		{toolOutcome{executed: true, runState: provider.ToolRunCompleted, output: "context canceled"}, provider.ToolRunCompleted},
	} {
		if got := outcomeRunState(tc.out); got != tc.want {
			t.Fatalf("got %s want %s", got, tc.want)
		}
	}
}
