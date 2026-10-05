package serve

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reasonix/internal/state/sessionstore"
	"testing"
	"time"

	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/eventwire"
	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/safety/permission"
	"reasonix/internal/session/control"
)

// A frontend correlates the projection with the request it already drew a card
// for, so the two have to name the same prompt. Ordinary tool permission used to
// be absent from the projection entirely — the card was there, the run was
// blocked on it, and /status said nothing was waiting.
func TestStatusProjectsThePendingApprovalTheEventAnnounced(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(serveApprovalWriter{})
	ag := agent.New(&serveApprovalProvider{}, reg, sessionstore.NewSession(""), agent.Options{}, event.Discard)
	bc := NewBroadcaster()
	ctrl := control.New(control.Options{
		Runner: ag, Executor: ag, Sink: bc,
		Policy: permission.New("ask", nil, nil, nil),
	})
	ctrl.EnableInteractiveApproval()
	srv := httptest.NewServer(operatorHandler(New(ctrl, bc, config.ServeConfig{})))
	defer srv.Close()

	frames, cancel := bc.Subscribe()
	defer cancel()
	go ctrl.Executor().Run(context.Background(), "write a file")

	announced := ""
	deadline := time.After(5 * time.Second)
	for announced == "" {
		select {
		case f := <-frames:
			var wire eventwire.Event
			if json.Unmarshal(f.Data, &wire) == nil && wire.Kind == "approval_request" && wire.Approval != nil {
				announced = wire.Approval.ID
			}
		case <-deadline:
			t.Fatal("no approval_request reached a subscriber")
		}
	}

	resp, err := http.Get(srv.URL + "/status")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var status struct {
		Decisions []control.Decision `json:"decisions"`
	}
	if err := json.Unmarshal(body, &status); err != nil {
		t.Fatalf("status is not json: %v", err)
	}
	if len(status.Decisions) != 1 {
		t.Fatalf("status.decisions = %+v, want the one prompt the run is blocked on", status.Decisions)
	}
	if got := status.Decisions[0]; got.ID != announced || got.Kind != control.DecisionToolApproval {
		t.Fatalf("status projected %+v, want id %q as %q", got, announced, control.DecisionToolApproval)
	}
}
