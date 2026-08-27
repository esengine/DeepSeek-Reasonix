package serve

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/eventwire"
)

type pendingPromptAPI struct {
	control.SessionAPI
	compactInstructions string
}

func (p *pendingPromptAPI) ReplayPendingPromptsTo(sink event.Sink) {
	sink.Emit(event.Event{Kind: event.ApprovalRequest, Approval: event.Approval{ID: "approval-1", Tool: "bash", Subject: "run tests"}})
}

func (p *pendingPromptAPI) Compact(_ context.Context, instructions string) error {
	p.compactInstructions = instructions
	return nil
}

func TestServeRecoveryEndpointsFailClosedAndExposeRuntime(t *testing.T) {
	bc := NewBroadcaster()
	ctrl := control.New(control.Options{Sink: bc})
	ctrl.SetGoal("ship remote parity")
	api := &pendingPromptAPI{SessionAPI: ctrl}
	srv := httptest.NewServer(New(api, bc, config.ServeConfig{}).Handler())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/pending-prompts")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var prompts []eventwire.Event
	if err := json.NewDecoder(resp.Body).Decode(&prompts); err != nil || len(prompts) != 1 || prompts[0].Approval == nil || prompts[0].Approval.ID != "approval-1" {
		t.Fatalf("pending prompts = %+v, err %v", prompts, err)
	}

	resp, err = http.Get(srv.URL + "/status")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var status struct {
		GoalRuntime *control.GoalRuntimeView `json:"goalRuntime"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil || status.GoalRuntime == nil {
		t.Fatalf("goal runtime = %+v, err %v", status.GoalRuntime, err)
	}
	resp, err = http.Post(srv.URL+"/compact", "application/json", strings.NewReader(`{"instructions":"preserve tests"}`))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusNoContent || api.compactInstructions != "preserve tests" {
		t.Fatalf("compact = status:%v instructions:%q", resp.StatusCode, api.compactInstructions)
	}
	resp.Body.Close()

	resp, err = http.Post(srv.URL+"/rewind", "application/json", strings.NewReader(`{"turn":0,"scope":"conversationn"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid rewind scope status = %d, want 400", resp.StatusCode)
	}
}
