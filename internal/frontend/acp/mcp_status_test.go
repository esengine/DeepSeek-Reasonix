package acp

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/event"
	"reasonix/internal/ext/plugin"
	"reasonix/internal/session/control"
)

type mcpStatusFactory struct {
	host *plugin.Host
}

func (f *mcpStatusFactory) NewSession(_ context.Context, p SessionParams) (*control.Controller, error) {
	f.host = plugin.NewHost()
	f.host.SetStatusSink(p.Sink)
	runner := &fakeRunner{sink: p.Sink, behavior: func(context.Context, event.Sink, string) error { return nil }}
	return control.New(control.Options{Runner: runner, Sink: p.Sink, Host: f.host, WorkspaceRoot: p.Cwd}), nil
}

func waitMCPStatusUpdate(t *testing.T, client *rpcClient) ReasonixMCPStatus {
	t.Helper()
	deadline := time.After(testenv.Budget(t))
	for {
		select {
		case notification := <-client.notifs:
			if notification.Method != sessionMCPStatusUpdateMethod {
				continue
			}
			var status ReasonixMCPStatus
			if err := json.Unmarshal(notification.Params, &status); err != nil {
				t.Fatal(err)
			}
			return status
		case <-deadline:
			t.Fatal("MCP status update not delivered")
			return ReasonixMCPStatus{}
		}
	}
}

func TestMCPStatusExtensionReportsSessionFailure(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	factory := &mcpStatusFactory{}
	client, stop := startServer(t, factory)
	defer stop()
	resp := client.call(t, "initialize", InitializeParams{ProtocolVersion: 1})
	var initialized InitializeResult
	if err := json.Unmarshal(resp.Result, &initialized); err != nil {
		t.Fatal(err)
	}
	for _, method := range []string{sessionMCPStatusMethod, sessionMCPStatusUpdateMethod} {
		capability, ok := initialized.AgentCapabilities.Meta[method].(map[string]any)
		if !ok || capability["schemaVersion"] != float64(mcpStatusSchemaVersion) {
			t.Fatalf("%s capability = %#v", method, initialized.AgentCapabilities.Meta[method])
		}
	}
	id := openStatusSession(t, client, testenv.TempDir(t))
	initial := waitMCPStatusUpdate(t, client)
	if initial.SessionID != id || initial.SchemaVersion != 1 || initial.Servers == nil || len(initial.Servers) != 0 {
		t.Fatalf("initial MCP status = %+v", initial)
	}
	factory.host.RecordFailure(plugin.Spec{Name: "editor-tools", Type: "http"}, errors.New("connection refused"))
	update := waitMCPStatusUpdate(t, client)
	if update.SessionID != id || len(update.Servers) != 1 || update.Servers[0].Name != "editor-tools" || update.Servers[0].Status != "failed" || update.Servers[0].Error == "" {
		t.Fatalf("failure update = %+v", update)
	}
	query := client.call(t, sessionMCPStatusMethod, SessionStatusParams{SessionID: id})
	if query.Error != nil {
		t.Fatalf("MCP status query: %+v", query.Error)
	}
	var snapshot ReasonixMCPStatus
	if err := json.Unmarshal(query.Result, &snapshot); err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Servers) != 1 || snapshot.Servers[0].Status != "failed" {
		t.Fatalf("MCP status snapshot = %+v", snapshot)
	}
}

func TestMCPStatusExtensionReportsLaunchApprovalAsPending(t *testing.T) {
	t.Setenv("REASONIX_HOME", testenv.TempDir(t))
	factory := &mcpStatusFactory{}
	client, stop := startServer(t, factory)
	defer stop()
	client.call(t, "initialize", InitializeParams{ProtocolVersion: 1})
	id := openStatusSession(t, client, testenv.TempDir(t))
	waitMCPStatusUpdate(t, client)
	factory.host.RecordLaunchApprovalRequired(plugin.Spec{Name: "project-tools", Type: "stdio"})
	update := waitMCPStatusUpdate(t, client)
	if update.SessionID != id || len(update.Servers) != 1 || update.Servers[0].Status != "pending" || update.Servers[0].Error == "" {
		t.Fatalf("launch approval notification = %+v", update)
	}
	query := client.call(t, sessionMCPStatusMethod, SessionStatusParams{SessionID: id})
	if query.Error != nil {
		t.Fatalf("MCP status query: %+v", query.Error)
	}
	var snapshot ReasonixMCPStatus
	if err := json.Unmarshal(query.Result, &snapshot); err != nil {
		t.Fatal(err)
	}
	if len(snapshot.Servers) != 1 || snapshot.Servers[0].Status != "pending" {
		t.Fatalf("launch approval snapshot = %+v", snapshot)
	}
}
