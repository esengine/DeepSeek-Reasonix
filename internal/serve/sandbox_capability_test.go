package serve

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/sandboxauth"
	"reasonix/internal/tool"
)

func newCapabilityServer(t *testing.T, ctrl *control.Controller) *httptest.Server {
	t.Helper()
	bc := NewBroadcaster()
	srv := httptest.NewServer(New(ctrl, bc, config.ServeConfig{}).Handler())
	t.Cleanup(srv.Close)
	return srv
}

func postCapabilityJSON(t *testing.T, url string, body any) *http.Response {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.Post(url, "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func TestCapabilityApproveHandlerValidatesAndKeepsInvalidDecisionsPending(t *testing.T) {
	ids := make(chan string, 1)
	ctrl := control.New(control.Options{Sink: event.FuncSink(func(e event.Event) {
		if e.Kind == event.ApprovalRequest && e.Approval.Kind == sandboxauth.ApprovalKind {
			ids <- e.Approval.ID
		}
	})})
	ctrl.EnableInteractiveApproval()
	srv := newCapabilityServer(t, ctrl)

	decision := make(chan sandboxauth.Action, 1)
	go func() {
		action, _ := ctrl.ApproveSandboxCapability(context.Background(), sandboxauth.Prompt{})
		decision <- action
	}()

	var id string
	select {
	case id = <-ids:
	case <-time.After(time.Second):
		t.Fatal("capability prompt was not emitted")
	}

	resp := postCapabilityJSON(t, srv.URL+"/capability-approve", map[string]any{"id": id, "action": "surprise"})
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid action status = %d, want 400", resp.StatusCode)
	}

	resp = postCapabilityJSON(t, srv.URL+"/capability-approve", map[string]any{"id": id, "action": sandboxauth.RunSandboxed})
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("valid action status = %d, want 204", resp.StatusCode)
	}
	select {
	case got := <-decision:
		if got != sandboxauth.RunSandboxed {
			t.Fatalf("decision = %q, want %q", got, sandboxauth.RunSandboxed)
		}
	case <-time.After(time.Second):
		t.Fatal("valid action did not resolve prompt")
	}

	resp = postCapabilityJSON(t, srv.URL+"/capability-approve", map[string]any{"id": id, "action": sandboxauth.RunSandboxed})
	resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("stale action status = %d, want 409", resp.StatusCode)
	}

	resp = postCapabilityJSON(t, srv.URL+"/capability-approve", map[string]any{"action": sandboxauth.RunSandboxed})
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("missing id status = %d, want 400", resp.StatusCode)
	}
}

func TestYOLOAcknowledgeHandlerReturnsAuthoritativeStateWithoutReload(t *testing.T) {
	workspace := t.TempDir()
	policy := sandboxauth.NewYOLOPolicy(sandboxauth.YOLOPolicyConfig{
		Workspace: workspace, Effective: true, ProjectExpansion: true,
	})
	ctrl := control.New(control.Options{
		WorkspaceRoot:           workspace,
		SandboxCapabilityEngine: &sandboxauth.Engine{AutoOnce: policy},
		Executor:                agent.New(nil, tool.NewRegistry(), agent.NewSession("sys"), agent.Options{}, event.Discard),
	})
	ctrl.EnableInteractiveApproval()
	ctrl.SetToolApprovalMode(control.ToolApprovalYolo)
	srv := newCapabilityServer(t, ctrl)

	resp := postCapabilityJSON(t, srv.URL+"/yolo-acknowledge", map[string]any{"accept": true})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("ack status = %d, want 200", resp.StatusCode)
	}
	var got struct {
		Resolved bool                        `json:"resolved"`
		State    sandboxauth.YOLOPolicyState `json:"state"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if !got.Resolved || got.State.Acknowledgement != sandboxauth.YOLOAccepted {
		t.Fatalf("ack response = %+v, want resolved accepted", got)
	}

	resp = postCapabilityJSON(t, srv.URL+"/yolo-acknowledge", map[string]any{"accept": false})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("repeat ack status = %d, want 409", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.Resolved || got.State.Acknowledgement != sandboxauth.YOLOAccepted {
		t.Fatalf("repeat ack response = %+v, want unresolved accepted", got)
	}
}

func TestYOLOAcknowledgeHandlerRejectsBadBodyAndUnavailablePolicy(t *testing.T) {
	ctrl := control.New(control.Options{})
	srv := newCapabilityServer(t, ctrl)

	resp := postCapabilityJSON(t, srv.URL+"/yolo-acknowledge", map[string]any{})
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("missing accept status = %d, want 400", resp.StatusCode)
	}

	resp = postCapabilityJSON(t, srv.URL+"/yolo-acknowledge", map[string]any{"accept": true})
	resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("unavailable policy status = %d, want 503", resp.StatusCode)
	}
}

func TestStatusPublishesTypedYOLOPolicyAcrossPostureAndAcknowledgements(t *testing.T) {
	workspace := t.TempDir()
	policy := sandboxauth.NewYOLOPolicy(sandboxauth.YOLOPolicyConfig{
		Workspace: workspace, Effective: true, ProjectExpansion: true,
	})
	ctrl := control.New(control.Options{
		WorkspaceRoot:           workspace,
		SandboxCapabilityEngine: &sandboxauth.Engine{AutoOnce: policy},
		Executor:                agent.New(nil, tool.NewRegistry(), agent.NewSession("sys"), agent.Options{}, event.Discard),
	})
	ctrl.EnableInteractiveApproval()
	srv := newCapabilityServer(t, ctrl)

	readState := func() sandboxauth.YOLOPolicyState {
		resp, err := http.Get(srv.URL + "/status")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var status struct {
			State sandboxauth.YOLOPolicyState `json:"sandboxCapabilityYOLO"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
			t.Fatal(err)
		}
		return status.State
	}

	if state := readState(); state.YOLO || !state.Interactive || state.Acknowledgement != sandboxauth.YOLORequired {
		t.Fatalf("ask state = %+v", state)
	}
	resp := postCapabilityJSON(t, srv.URL+"/tool-approval-mode", map[string]any{"mode": control.ToolApprovalYolo})
	resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("hot-yolo status = %d, want 204", resp.StatusCode)
	}
	if state := readState(); !state.YOLO || state.Acknowledgement != sandboxauth.YOLORequired {
		t.Fatalf("hot-yolo state = %+v", state)
	}
	ctrl.AcknowledgeSandboxCapabilityYOLO(true)
	if state := readState(); state.Acknowledgement != sandboxauth.YOLOAccepted {
		t.Fatalf("accepted state = %+v", state)
	}
	policy.ClearSessionState()
	ctrl.AcknowledgeSandboxCapabilityYOLO(false)
	if state := readState(); state.Acknowledgement != sandboxauth.YOLORefused {
		t.Fatalf("refused state = %+v", state)
	}

	plainPolicy := sandboxauth.NewYOLOPolicy(sandboxauth.YOLOPolicyConfig{Workspace: workspace, Effective: true})
	plain := control.New(control.Options{
		SandboxCapabilityEngine: &sandboxauth.Engine{AutoOnce: plainPolicy},
		Executor:                agent.New(nil, tool.NewRegistry(), agent.NewSession("sys"), agent.Options{}, event.Discard),
	})
	plainSrv := newCapabilityServer(t, plain)
	resp, err := http.Get(plainSrv.URL + "/status")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var status struct {
		State sandboxauth.YOLOPolicyState `json:"sandboxCapabilityYOLO"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&status); err != nil {
		t.Fatal(err)
	}
	if status.State.Acknowledgement != sandboxauth.YOLONotRequired {
		t.Fatalf("non-expanding state = %+v", status.State)
	}
}
