package agent

import (
	"context"
	"encoding/json"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
	"testing"
)

type recoveryVerifierTool struct {
	tool.Tool
	states map[string]tool.WriteVerification
}

func (*recoveryVerifierTool) ReadOnly() bool { return false }
func (v *recoveryVerifierTool) VerifyWrite(_ context.Context, i tool.FileWriteIntent) tool.WriteVerification {
	return v.states[i.Path]
}

func TestRecoveredWriteRechecksEveryTargetBeforeSkipping(t *testing.T) {
	call := provider.ToolCall{Name: "write", Arguments: `{"a":1}`, WriteIntents: []json.RawMessage{
		json.RawMessage(`{"version":1,"path":"/a","host":"test","after":"a"}`),
		json.RawMessage(`{"version":1,"path":"/b","host":"test","after":"b"}`),
	}}
	v := &recoveryVerifierTool{states: map[string]tool.WriteVerification{"/a": tool.WriteSatisfied, "/b": tool.WriteConflict}}
	turn := &turnRuntime{writeRecovery: map[string]provider.ToolCall{writeRecoveryKey(call): call}}
	check := func(wantBlocked bool) {
		t.Helper()
		got, handled := recoverPreviousWrite(context.Background(), turn, call, v)
		if !handled || got.blocked != wantBlocked {
			t.Fatalf("handled=%v outcome=%+v", handled, got)
		}
	}
	check(true)
	v.states["/b"] = tool.WriteSatisfied
	check(false)
	v.states["/a"] = tool.WriteConflict
	check(true)
}
