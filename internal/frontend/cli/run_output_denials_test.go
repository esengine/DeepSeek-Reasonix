package cli

import (
	"bytes"
	"encoding/json"
	"testing"
	"time"

	"reasonix/internal/contract/event"
	"reasonix/internal/safety/permission"
)

// A headless run whose writes were refused still ends in success; the result
// says which calls a permission gate refused and why, by code rather than prose.
func TestRunResultListsPermissionDenials(t *testing.T) {
	var out bytes.Buffer
	sink := newRunOutputSink(&out, runOutputJSON)
	sink.Emit(event.Event{Kind: event.ToolResult, Tool: event.Tool{ID: "c1", Name: "write_file", Err: "blocked", RefusalCode: permission.RefusalUnattended}})
	sink.Emit(event.Event{Kind: event.ToolResult, Tool: event.Tool{ID: "c2", Name: "bash", Err: "exit 1"}})
	sink.Emit(event.Event{Kind: event.ToolResult, Tool: event.Tool{ID: "c3", Name: "edit_file", Err: "stale", RefusalCode: "tool.stale_anchor"}})
	sink.Emit(event.Event{Kind: event.Message, Text: "could not write"})
	sink.SetPermissionMode("ask")
	if err := sink.Finalize("s", time.Now(), nil); err != nil {
		t.Fatal(err)
	}
	var got struct {
		IsError bool                  `json:"is_error"`
		Mode    string                `json:"permission_mode"`
		Denials []runPermissionDenial `json:"permission_denials"`
	}
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("%v: %s", err, out.String())
	}
	want := []runPermissionDenial{{ToolName: "write_file", ToolUseID: "c1", Code: permission.RefusalUnattended}}
	if got.IsError || got.Mode != "ask" || len(got.Denials) != 1 || got.Denials[0] != want[0] {
		t.Fatalf("result = %+v, want only the permission refusal listed and no error", got)
	}

	out.Reset()
	clean := newRunOutputSink(&out, runOutputJSON)
	clean.Emit(event.Event{Kind: event.Message, Text: "ok"})
	if err := clean.Finalize("s", time.Now(), nil); err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(out.Bytes(), []byte(`"permission_denials":[]`)) {
		t.Fatalf("a run with no refusals still carries an empty list: %s", out.String())
	}
}
