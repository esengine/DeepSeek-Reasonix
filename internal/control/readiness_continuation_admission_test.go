package control

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/event"
	"reasonix/internal/extension"
	"reasonix/internal/extension/dispatch"
	"reasonix/internal/extension/protocol"
	"reasonix/internal/provider"
)

func TestBlockedReadinessContinuationRemainsRecoverable(t *testing.T) {
	client := &fakeExtClient{interceptFn: func(_ protocol.InterceptEvent, raw json.RawMessage) (protocol.InterceptResult, error) {
		var payload dispatch.InputPayload
		if err := json.Unmarshal(raw, &payload); err != nil {
			return protocol.InterceptResult{}, err
		}
		if strings.Contains(payload.Text, agent.ReadinessContinuationPrefix) {
			return protocol.InterceptResult{Decision: protocol.DecisionBlock, Reason: "synthetic turns disabled"}, nil
		}
		return protocol.InterceptResult{Decision: protocol.DecisionContinue}, nil
	}}
	c, prov := readinessContinuationController(t, [][]provider.Chunk{
		{toolCallChunk("write", "write_file", `{"path":"main.go","content":"package main"}`), {Type: provider.ChunkDone}},
		textTurn("implemented without checks"),
	}, event.Discard)
	c.SetExtensions(newExtensionTestDispatcher(client, []extension.InterceptorPoint{extension.PointInputReceive}, nil))

	err := newTurnOrchestrator(c).runGoalLoopWithRawDisplay(context.Background(), "update main.go", "update main.go", "")
	var readinessErr *agent.FinalReadinessError
	if !errors.As(err, &readinessErr) {
		t.Fatalf("blocked continuation error = %v, want the original readiness failure", err)
	}
	if readinessErr.Attempts != 1 {
		t.Fatalf("readiness attempts = %d, want only the original completed turn", readinessErr.Attempts)
	}
	if prov.call != 2 {
		t.Fatalf("provider calls = %d, want no request for the blocked continuation", prov.call)
	}
	if !c.executor.PrepareFinalReadinessRecovery() {
		t.Fatal("blocked continuation consumed the manual recovery action")
	}
}
