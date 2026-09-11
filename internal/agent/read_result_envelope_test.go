package agent

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
	"reasonix/internal/tool"
)

// envelopeReader is a stand-in reader that reports a fixed delivery, so the
// wiring under test is the host's identity stamping and transport clipping.
type envelopeReader struct{ env tool.ReadResultEnvelope }

func (envelopeReader) Name() string                                             { return "read_file" }
func (envelopeReader) Description() string                                      { return "fake reader" }
func (envelopeReader) Schema() json.RawMessage                                  { return json.RawMessage(`{"type":"object"}`) }
func (envelopeReader) ReadOnly() bool                                           { return true }
func (envelopeReader) Execute(context.Context, json.RawMessage) (string, error) { return "", nil }
func (r envelopeReader) ReadEnvelope(context.Context, json.RawMessage, string) (tool.ReadResultEnvelope, bool) {
	return r.env, true
}

type plainReader struct{}

func (plainReader) Name() string                                             { return "read_file" }
func (plainReader) Description() string                                      { return "plain reader" }
func (plainReader) Schema() json.RawMessage                                  { return json.RawMessage(`{"type":"object"}`) }
func (plainReader) ReadOnly() bool                                           { return true }
func (plainReader) Execute(context.Context, json.RawMessage) (string, error) { return "", nil }

func newEnvelopeTestAgent(t *testing.T, reader tool.Tool) (*Agent, *Session) {
	t.Helper()
	reg := tool.NewRegistry()
	reg.Add(reader)
	sess := NewSession("system")
	a := New(&userInputCaptureProvider{}, reg, sess, Options{WorkspaceID: "ws-1"}, event.Discard)
	a.reads.tasks = newReadTasks("test-session", 1)
	return a, sess
}

func storedEnvelope(t *testing.T, sess *Session) tool.ReadResultEnvelope {
	t.Helper()
	stored := sess.Snapshot()
	if len(stored) == 0 {
		t.Fatal("no stored messages")
	}
	last := stored[len(stored)-1]
	if len(last.ReadResult) == 0 {
		t.Fatal("tool message carries no read envelope")
	}
	var env tool.ReadResultEnvelope
	if err := json.Unmarshal(last.ReadResult, &env); err != nil {
		t.Fatalf("envelope is not valid JSON: %v", err)
	}
	return env
}

func TestStoreBatchToolResultStampsReaderEnvelope(t *testing.T) {
	reader := envelopeReader{env: tool.ReadResultEnvelope{
		ProtocolVersion: tool.ReadResultProtocolVersion,
		Source:          tool.ReadResultSource{CanonicalPath: "/w/a.go", Snapshot: "ss2:abc"},
		Intent:          tool.ReadIntentInspect,
		DeliveredRanges: []tool.ReadRange{{Start: 0, End: 2}},
		EOF:             true,
	}}
	a, sess := newEnvelopeTestAgent(t, reader)
	a.storeBatchToolResult(context.Background(), provider.ToolCall{ID: "c1", Name: "read_file", Arguments: `{"path":"a.go"}`}, toolOutcome{output: "  1→a\n  2→b\n"})

	env := storedEnvelope(t, sess)
	if env.ReadID == "" || env.ResultRef == "" {
		t.Fatalf("host must stamp read identity: %+v", env)
	}
	if env.Source.WorkspaceID != "ws-1" {
		t.Fatalf("WorkspaceID = %q, want ws-1", env.Source.WorkspaceID)
	}
	if env.ProtocolVersion != tool.ReadResultProtocolVersion || env.TransportCut != tool.ReadCutNone {
		t.Fatalf("untruncated delivery must stay uncut: %+v", env)
	}
}

func TestStoreBatchToolResultClipsEnvelopeToVisibleBytes(t *testing.T) {
	reader := envelopeReader{env: tool.ReadResultEnvelope{
		ProtocolVersion: tool.ReadResultProtocolVersion,
		Source:          tool.ReadResultSource{CanonicalPath: "/w/a.go", Snapshot: "ss2:abc"},
		DeliveredRanges: []tool.ReadRange{{Start: 0, End: 2}},
		EOF:             true,
	}}
	a, sess := newEnvelopeTestAgent(t, reader)
	a.storeBatchToolResult(context.Background(),
		provider.ToolCall{ID: "c1", Name: "read_file", Arguments: `{"path":"a.go"}`},
		toolOutcome{output: "  1→a\n", rawOutput: "  1→a\n  2→b\n", truncated: true},
	)

	env := storedEnvelope(t, sess)
	if env.TransportCut != tool.ReadCutToolOutput {
		t.Fatalf("TransportCut = %q, want %q", env.TransportCut, tool.ReadCutToolOutput)
	}
	if got := env.DeliveredRanges; len(got) != 1 || got[0] != (tool.ReadRange{Start: 0, End: 1}) {
		t.Fatalf("DeliveredRanges = %+v, want only the visible line", got)
	}
	if !env.HasMore || env.EOF {
		t.Fatalf("clipped delivery must be resumable: has_more=%v eof=%v", env.HasMore, env.EOF)
	}
}

func TestStoreBatchToolResultOmitsEnvelopeForPlainReaders(t *testing.T) {
	a, sess := newEnvelopeTestAgent(t, plainReader{})
	a.storeBatchToolResult(context.Background(), provider.ToolCall{ID: "c1", Name: "read_file", Arguments: `{"path":"a.go"}`}, toolOutcome{output: "  1→a\n"})

	stored := sess.Snapshot()
	if len(stored) == 0 {
		t.Fatal("no stored messages")
	}
	if len(stored[len(stored)-1].ReadResult) != 0 {
		t.Fatal("a reader that cannot describe its delivery must not get a fabricated envelope")
	}
}

func TestReadContinuationCursorJoinsTheLogicalRead(t *testing.T) {
	a, _ := newEnvelopeTestAgent(t, envelopeReader{})
	path := filepath.Join(t.TempDir(), "a.go")
	a.reads.tasks.remember("ir-1", tool.ReadResultEnvelope{
		Source: tool.ReadResultSource{CanonicalPath: path, Snapshot: "ss2:abc"},
	})
	cursor := tool.EncodeReadCursor(tool.ReadCursor{
		Path: path, ReadID: "ir-1", Snapshot: "ss2:abc",
		NextStart: 5, RequestEnd: 10, SessionID: "test-session", RunGen: 1,
		Binding: a.reads.tasks.binding,
	})
	a.reads.tasks.remember("ir-1", tool.ReadResultEnvelope{Source: tool.ReadResultSource{CanonicalPath: path, Snapshot: "ss2:abc"}, NextCursor: cursor})
	argsJSON, err := json.Marshal(map[string]string{"path": path, "cursor": cursor})
	if err != nil {
		t.Fatal(err)
	}
	plan := &toolCallPlan{execArgs: argsJSON}

	if out, blocked := a.resolveReadCursor(plan); blocked {
		t.Fatalf("a valid continuation cursor was rejected: %+v", out)
	}
	if plan.readTaskID != "ir-1" {
		t.Fatalf("readTaskID = %q, want the continued read ir-1", plan.readTaskID)
	}
	var args map[string]any
	if err := json.Unmarshal(plan.execArgs, &args); err != nil {
		t.Fatal(err)
	}
	if _, present := args["cursor"]; present {
		t.Fatal("the cursor must be consumed, not forwarded to the reader")
	}
	if args["offset"] != float64(5) || args["limit"] != float64(5) {
		t.Fatalf("rewritten args = %v, want offset 5 limit 5", args)
	}
}

func TestReadContinuationCursorRejections(t *testing.T) {
	cases := []struct {
		name   string
		change func(*tool.ReadCursor)
	}{
		{"malformed", nil},
		{"unknown read task", func(c *tool.ReadCursor) { c.ReadID = "ir-other" }},
		{"other session", func(c *tool.ReadCursor) { c.SessionID = "someone-else" }},
		{"earlier run", func(c *tool.ReadCursor) { c.RunGen = 99 }},
		{"other file", func(c *tool.ReadCursor) { c.Path = filepath.Join(filepath.Dir(c.Path), "b.go") }},
		{"changed content", func(c *tool.ReadCursor) { c.Snapshot = "ss2:zzz" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			a, _ := newEnvelopeTestAgent(t, envelopeReader{})
			path := filepath.Join(t.TempDir(), "a.go")
			cursor := tool.ReadCursor{Path: path, ReadID: "ir-1", Snapshot: "ss2:abc", NextStart: 5, RequestEnd: 10, SessionID: "test-session", RunGen: 1, Binding: a.reads.tasks.binding}
			a.reads.tasks.remember("ir-1", tool.ReadResultEnvelope{
				Source: tool.ReadResultSource{CanonicalPath: path, Snapshot: "ss2:abc"}, NextCursor: tool.EncodeReadCursor(cursor),
			})
			newPlan := func(token string) *toolCallPlan {
				args, err := json.Marshal(map[string]string{"path": path, "cursor": token})
				if err != nil {
					t.Fatal(err)
				}
				return &toolCallPlan{execArgs: args}
			}
			if out, blocked := a.resolveReadCursor(newPlan(tool.EncodeReadCursor(cursor))); blocked {
				t.Fatalf("unmodified cursor must be accepted before testing rejection: %+v", out)
			}
			token := "rc2:!!!"
			if tc.change != nil {
				tc.change(&cursor)
				token = tool.EncodeReadCursor(cursor)
			}
			plan := newPlan(token)
			out, blocked := a.resolveReadCursor(plan)
			if !blocked || !out.blocked {
				t.Fatalf("cursor %q must be rejected, got %+v (blocked=%v)", token, out, blocked)
			}
			if plan.readTaskID != "" {
				t.Fatalf("a rejected cursor must not select a read task, got %q", plan.readTaskID)
			}
		})
	}
}

func TestReadContinuationCursorAbsentIsNotABlock(t *testing.T) {
	a, _ := newEnvelopeTestAgent(t, envelopeReader{})
	plan := &toolCallPlan{execArgs: json.RawMessage(`{"path":"/w/a.go"}`)}
	if _, blocked := a.resolveReadCursor(plan); blocked {
		t.Fatal("a plain read must not be treated as a continuation")
	}
}

// TestModelInputMessagesStripsReadResult guards the single provider boundary:
// every request path goes through modelInputMessages, so no host envelope may
// survive it.
func TestModelInputMessagesStripsReadResult(t *testing.T) {
	msgs := []provider.Message{
		{Role: provider.RoleUser, Content: "look"},
		{Role: provider.RoleTool, ToolCallID: "c1", Name: "read_file", Content: "   1→a\n",
			ReadResult: json.RawMessage(`{"protocol_version":2,"read_id":"ir-1"}`)},
	}
	out := modelInputMessages(msgs)
	for i, msg := range out {
		if len(msg.ReadResult) != 0 {
			t.Fatalf("message %d leaked the read envelope into provider input: %s", i, msg.ReadResult)
		}
	}
}
