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

type strictRecordingTool struct {
	recordingTool
}

func (s *strictRecordingTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string"}},"required":["path"],"additionalProperties":false}`)
}

func TestInvalidArgumentsDoNotReachToolBeforeExtension(t *testing.T) {
	client := &fakeDispatchClient{}
	d := newExtDispatcher(client, true, nil, extension.PointToolBefore)
	rec := &strictRecordingTool{recordingTool: recordingTool{name: "read_file", readOnly: true}}
	reg := tool.NewRegistry()
	reg.Add(rec)
	a := New(nil, reg, NewSession(""), Options{Extensions: d}, event.Discard)
	out := a.executeOne(context.Background(), &a.turn, provider.ToolCall{Name: "read_file", Arguments: `{"unexpected":true}`})
	if out.errMsg == "" || !strings.Contains(out.output, "argument validation failed") {
		t.Fatalf("outcome = %+v, want host validation failure", out)
	}
	if rec.execs != 0 {
		t.Fatalf("invalid tool executed %d times", rec.execs)
	}
	if n := client.notifyCountFor(protocol.EventToolBefore); n != 0 {
		t.Fatalf("tool.before sidecar calls = %d, want 0 before valid arguments", n)
	}
}
