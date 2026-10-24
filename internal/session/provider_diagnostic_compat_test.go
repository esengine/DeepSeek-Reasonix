package session

import (
	"encoding/json"
	"os"
	"testing"

	"reasonix/internal/provider"
)

// This test is also copied unchanged into the v1.39.8 and v1.39.9 source
// snapshots by scripts/check-provider-diagnostic-compatibility.sh. Each reader
// must replay the other writer's actual framed store, then append and reopen it.
func TestProviderDiagnosticCrossVersionFixture(t *testing.T) {
	dir := os.Getenv("REASONIX_COMPAT_STORE")
	if dir == "" {
		t.Skip("run scripts/check-provider-diagnostic-compatibility.sh")
	}
	kind := os.Getenv("REASONIX_COMPAT_KIND")
	mode := os.Getenv("REASONIX_COMPAT_MODE")
	s, err := Open(dir, "provider-compat")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close(t.Context()) })
	if mode == "write" {
		cause := ""
		if kind == "image_input" {
			cause = "Image processing failed: configured image understanding model does not accept images"
		}
		payload, err := json.Marshal(map[string]any{"message": provider.Message{
			ID: "failure", Role: provider.RoleTool, LocalOnly: true,
			InterruptedTurn: &provider.InterruptedTurnRecovery{Pending: true, TerminalStatus: "failed", Cause: cause, FailureDiagnostic: &provider.FailureDiagnostic{Kind: kind, ProviderID: "deepseek"}},
		}})
		if err != nil {
			t.Fatal(err)
		}
		diagnostic := json.RawMessage(`{"schemaVersion":1,"requests":[]}`)
		if kind == "request_send_timeout" {
			diagnostic = json.RawMessage(`{"schemaVersion":2,"requests":[{"errorCode":"request_send_timeout","errorPhase":"request_write","timeoutSource":"send_guard","sendTimeoutMs":300000,"sendElapsedMs":300001}]}`)
		}
		_, err = s.Append(t.Context(), Batch{OperationID: "fixture", Events: []Event{
			{Kind: "message/complete", Payload: payload},
			{Kind: "diagnostic/provider", Optional: true, Payload: diagnostic},
		}})
		if err != nil {
			t.Fatal(err)
		}
	} else {
		commits, err := Replay(dir, nil)
		if err != nil {
			t.Fatal(err)
		}
		projection, err := Project(commits)
		if err != nil {
			t.Fatal(err)
		}
		messages := projection.Messages
		if len(messages) != 1 || messages[0].InterruptedTurn == nil || messages[0].InterruptedTurn.FailureDiagnostic.Kind != kind {
			t.Fatalf("old/new reader lost failure: %+v", messages)
		}
		if kind == "image_input" && messages[0].InterruptedTurn.Cause != "Image processing failed: configured image understanding model does not accept images" {
			t.Fatal("reader lost original image cause")
		}
		payload, _ := json.Marshal(map[string]any{"message": provider.Message{ID: "continued", Role: provider.RoleUser, Content: "continue"}})
		if _, err = s.Append(t.Context(), Batch{OperationID: "continue", Events: []Event{{Kind: "message/complete", Payload: payload}}}); err != nil {
			t.Fatal(err)
		}
	}
	if err = s.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	commits, err := Replay(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	projection, err := Project(commits)
	want := 1
	if mode != "write" {
		want = 2
	}
	if err != nil || len(projection.Messages) != want {
		t.Fatalf("reopen: messages=%d err=%v", len(projection.Messages), err)
	}
}
