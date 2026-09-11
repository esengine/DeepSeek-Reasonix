package main

import (
	"encoding/json"
	"strings"
	"testing"

	"reasonix/internal/plugin"
)

func TestMCPServerViewSessionDiagnosticsRemainOptional(t *testing.T) {
	legacy, err := json.Marshal(ServerView{Name: "legacy", Status: "connected"})
	if err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"protocolVersion", "sessionState", "reconnectAttempts", "errorKind"} {
		if strings.Contains(string(legacy), field) {
			t.Fatalf("legacy payload unexpectedly contains optional %s: %s", field, legacy)
		}
	}

	var current ServerView
	if err := json.Unmarshal([]byte(`{
		"name":"current","status":"connected","protocolVersion":"2025-11-25",
		"sessionState":"reconnecting","reconnectAttempts":2,"errorKind":"session_missing"
	}`), &current); err != nil {
		t.Fatal(err)
	}
	if current.ProtocolVersion != "2025-11-25" || current.SessionState != "reconnecting" ||
		current.ReconnectAttempts != 2 || current.ErrorKind != "session_missing" {
		t.Fatalf("current session diagnostics = %+v", current)
	}

	for state, want := range map[plugin.SessionState]string{
		plugin.SessionStateConnecting: "connecting", plugin.SessionStateListening: "connecting",
		plugin.SessionStateReconnecting: "connecting", plugin.SessionStateReady: "ready",
		plugin.SessionStateFailed: "issue", plugin.SessionStateClosed: "ready",
	} {
		if got := mcpSessionRuntimeState(state); got != want {
			t.Fatalf("runtime state for %q = %q, want %q", state, got, want)
		}
	}
}
