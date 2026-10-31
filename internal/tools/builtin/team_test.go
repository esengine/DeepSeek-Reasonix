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
	if len(names) != 12 || !slices.Contains(names, "message_agent") || !slices.Contains(names, "hand_turn") {
		t.Fatalf("names = %v", names)
	}
	for _, tl := range TeamTools(nil) {
		var schema map[string]any
		if err := json.Unmarshal(tl.Schema(), &schema); err != nil {
			t.Fatalf("%s schema: %v", tl.Name(), err)
		}
	}
}
