package cli

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/frontend/acp"
	"reasonix/internal/state/sessionstore"
)

func TestACPPreparesAndRestoresRootEnvironmentThroughProtocol(t *testing.T) {
	root := rootRunFixture(t)
	serverWire, clientWire := net.Pipe()
	ctx, cancel := context.WithCancel(t.Context())
	serverDone, clientDone := make(chan error, 1), make(chan error, 1)
	client := acp.NewConn(clientWire, clientWire)
	client.Handle("session/request_permission", func(context.Context, json.RawMessage) (any, error) {
		return acp.PermissionRequestResult{Outcome: acp.PermissionOutcome{Outcome: "selected", OptionID: string(acp.OptAllowOnce)}}, nil
	})
	go func() {
		serverDone <- acp.Serve(ctx, serverWire, serverWire, &acpFactory{}, acp.AgentInfo{Name: "fixture", Version: "test"})
	}()
	go func() { clientDone <- client.Serve(ctx) }()
	t.Cleanup(func() { cancel(); serverWire.Close(); clientWire.Close(); <-serverDone; <-clientDone })
	call := func(method string, params any) json.RawMessage {
		t.Helper()
		raw, err := client.Request(ctx, method, params)
		if err != nil {
			t.Fatalf("%s: %v", method, err)
		}
		return raw
	}
	call("initialize", acp.InitializeParams{ProtocolVersion: 1})
	var opened acp.SessionNewResult
	if err := json.Unmarshal(call("session/new", acp.SessionNewParams{Cwd: root}), &opened); err != nil {
		t.Fatal(err)
	}
	var response acp.SessionPromptResult
	if err := json.Unmarshal(call("session/prompt", acp.SessionPromptParams{SessionID: opened.SessionID, Prompt: []acp.ContentBlock{{Type: "text", Text: "write a file"}}}), &response); err != nil {
		t.Fatal(err)
	}
	vendor, ok := response.Meta["reasonix.io"].(map[string]any)
	if !ok {
		t.Fatalf("missing transcript response: %+v", response)
	}
	path, _ := vendor["transcriptPath"].(string)
	binding, bound, err := sessionstore.LoadExecutionEnvironment(path)
	if err != nil || !bound {
		t.Fatalf("missing root attachment: %v %v", bound, err)
	}
	if raw, err := os.ReadFile(filepath.Join(binding.WorkspaceRoot, "from-run.txt")); err != nil || string(raw) != "isolated\n" {
		t.Fatalf("candidate output = %q %v", raw, err)
	}
	if _, err := os.Stat(filepath.Join(root, "from-run.txt")); !os.IsNotExist(err) {
		t.Fatal("ACP wrote source", err)
	}
	call("session/close", map[string]any{"sessionId": opened.SessionID})
	call("session/load", map[string]any{"sessionId": opened.SessionID, "cwd": root})
	call("session/prompt", acp.SessionPromptParams{SessionID: opened.SessionID, Prompt: []acp.ContentBlock{{Type: "text", Text: "continue"}}})
	restored, _, err := sessionstore.LoadExecutionEnvironment(path)
	if err != nil || restored != binding {
		t.Fatalf("restore replaced environment: %+v %v", restored, err)
	}
}
