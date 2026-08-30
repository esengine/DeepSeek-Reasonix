package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/extension"
	"reasonix/internal/extension/protocol"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

func TestAgentBeforeStartBlockReleasesFinalReadinessRecovery(t *testing.T) {
	client := &fakeDispatchClient{interceptFn: func(ev protocol.InterceptEvent, _ json.RawMessage) (protocol.InterceptResult, error) {
		if ev == protocol.EventAgentBeforeStart {
			return blockWith("retry later"), nil
		}
		return protocol.InterceptResult{Decision: protocol.DecisionContinue}, nil
	}}
	d := newExtDispatcher(client, true, nil, extension.PointAgentBeforeStart)
	sess := NewSession("sys")
	sess.Add(provider.Message{
		Role: provider.RoleTool, ToolCallID: provider.LocalOnlyToolID, Name: provider.LocalOnlyToolName, LocalOnly: true,
		FinalReadinessRecovery: &provider.FinalReadinessRecovery{
			Pending: true, Checkpoint: json.RawMessage(`{"receipts":[]}`),
		},
	})
	a := New(&mockProvider{name: "p"}, tool.NewRegistry(), sess, Options{Extensions: d}, event.Discard)
	a.pending.finalReadinessRecovery = true
	if !a.PrepareFinalReadinessRecovery() {
		t.Fatal("initial recovery preparation was rejected")
	}
	if err := a.Run(context.Background(), "continue checks"); err == nil || !strings.Contains(err.Error(), "retry later") {
		t.Fatalf("Run err = %v, want extension block", err)
	}
	if !a.PrepareFinalReadinessRecovery() {
		t.Fatal("blocked pre-start consumed the durable recovery action")
	}
}
