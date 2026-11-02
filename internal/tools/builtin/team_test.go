package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"testing"

	"reasonix/internal/contract/tool"
)

func TestTeamToolsRefuseOutsideAnAgentSession(t *testing.T) {
	for _, tl := range TeamTools(&tool.TeamLink{}) {
		_, err := tl.Execute(context.Background(), json.RawMessage(`{}`))
		var r tool.Refusal
		if !errors.As(err, &r) || r.Code != "team.disabled" {
			t.Errorf("%s unbound = %v", tl.Name(), err)
		}
	}
}

type stubTeam struct {
	tool.TeamDirectory
	sent tool.TeamMessage
}

func (s *stubTeam) MessageAgent(_ context.Context, m tool.TeamMessage) (tool.TeamReceipt, error) {
	s.sent = m
	return tool.TeamReceipt{Exchange: "x-1", ToName: "Ben", State: "delivered"}, nil
}

func TestTeamToolArgumentsAreDecodedOrRefused(t *testing.T) {
	link := &tool.TeamLink{}
	stub := &stubTeam{}
	link.Bind(stub)
	var send tool.Tool
	for _, tl := range TeamTools(link) {
		if tl.Name() == "message_agent" {
			send = tl
		}
	}
	out, err := send.Execute(context.Background(), json.RawMessage(`{"to":"Ben","message":"hi","when":"now","request_id":"r1"}`))
	if err != nil || stub.sent.To != "Ben" || !stub.sent.Now || stub.sent.RequestID != "r1" || out == "" {
		t.Fatalf("message_agent = %q, %v; sent %+v", out, err, stub.sent)
	}
	_, err = send.Execute(context.Background(), json.RawMessage(`{"to":3}`))
	var r tool.Refusal
	if !errors.As(err, &r) || r.Code != "team.invalid" {
		t.Fatalf("bad arguments = %v", err)
	}
}

func TestTeamToolNamesMatchTheRegisteredSet(t *testing.T) {
	names := TeamToolNames()
	if len(names) != 14 || !slices.Contains(names, "message_agent") || !slices.Contains(names, "hand_turn") || !slices.Contains(names, "remember_shared") {
		t.Fatalf("names = %v", names)
	}
	for _, tl := range TeamTools(nil) {
		var schema map[string]any
		if err := json.Unmarshal(tl.Schema(), &schema); err != nil {
			t.Fatalf("%s schema: %v", tl.Name(), err)
		}
	}
}

type sharedStub struct {
	tool.TeamDirectory
	tier, group string
	write       bool
	args        string
}

func (s *sharedStub) SharedMemory(_ context.Context, tier, group string, write bool) (tool.Tool, error) {
	s.tier, s.group, s.write = tier, group, write
	return recorder{s}, nil
}

type recorder struct{ s *sharedStub }

func (recorder) Name() string            { return "memory" }
func (recorder) Description() string     { return "" }
func (recorder) Schema() json.RawMessage { return json.RawMessage(`{}`) }
func (recorder) ReadOnly() bool          { return true }
func (r recorder) Execute(_ context.Context, args json.RawMessage) (string, error) {
	r.s.args = string(args)
	return "ok", nil
}

// The shared memory tools pick the store and hand the rest to the memory tool
// bound to it; a scope or an activation never reaches it, since a shared store
// has no global half and nothing it holds is pinned into a prefix.
func TestSharedMemoryToolsHandTheirArgumentsToTheBoundStore(t *testing.T) {
	link := &tool.TeamLink{}
	stub := &sharedStub{}
	link.Bind(stub)
	byName := map[string]tool.Tool{}
	for _, tl := range TeamTools(link) {
		byName[tl.Name()] = tl
	}
	if _, err := byName["remember_shared"].Execute(context.Background(), json.RawMessage(`{"tier":"group","group":"Launch","description":"d","body":"b","scope":"global","activation":"pinned"}`)); err != nil {
		t.Fatal(err)
	}
	var passed map[string]any
	_ = json.Unmarshal([]byte(stub.args), &passed)
	if stub.tier != "group" || stub.group != "Launch" || !stub.write || passed["body"] != "b" || passed["scope"] != nil || passed["activation"] != nil || passed["tier"] != nil {
		t.Fatalf("remember_shared reached %+v with %s", stub, stub.args)
	}
	if _, err := byName["recall_shared"].Execute(context.Background(), json.RawMessage(`{"tier":"team","operation":"search","query":"style"}`)); err != nil {
		t.Fatal(err)
	}
	if stub.tier != "team" || stub.write || !byName["recall_shared"].ReadOnly() || byName["remember_shared"].ReadOnly() {
		t.Fatalf("recall_shared reached %+v", stub)
	}
}
