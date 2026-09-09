package boot

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/config"
	"reasonix/internal/event"
)

func TestModelSettingsApprovalResumeKeepsAcceptedCredential(t *testing.T) {
	isolateConfigHome(t)
	root := robustTempDir(t)
	t.Chdir(root)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	var calls atomic.Int32
	keys := make(chan string, 4)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		keys <- r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "text/event-stream")
		if calls.Add(1) == 1 {
			fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"tool_calls\":[{\"index\":0,\"id\":\"write-approval\",\"type\":\"function\",\"function\":{\"name\":\"write_file\",\"arguments\":\"{\\\"path\\\":\\\"approved.txt\\\",\\\"content\\\":\\\"approved snapshot\\\"}\"}}]},\"finish_reason\":\"tool_calls\"}]}\n\ndata: [DONE]\n\n")
			return
		}
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"finished\"},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n")
	}))
	defer server.Close()
	if _, err := config.SetCredential("APPROVAL_SNAPSHOT_KEY", "before-save"); err != nil {
		t.Fatal(err)
	}
	cfg := config.Default()
	cfg.DefaultModel = "p/m"
	cfg.Providers = []config.ProviderEntry{{Name: "p", Kind: "openai", BaseURL: server.URL, Model: "m", APIKeyEnv: "APPROVAL_SNAPSHOT_KEY"}}
	cfg.Permissions.Mode, cfg.Permissions.Ask = "ask", []string{"write_file"}
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatal(err)
	}
	approval := make(chan string, 1)
	ctrl, err := Build(ctx, Options{WorkspaceRoot: root, Sink: event.FuncSink(func(e event.Event) {
		if e.Kind == event.ApprovalRequest {
			approval <- e.Approval.ID
		}
	})})
	if err != nil {
		t.Fatal(err)
	}
	defer ctrl.Close()
	ctrl.EnableInteractiveApproval()
	done := make(chan error, 1)
	go func() { done <- ctrl.RunTurn(ctx, "write approved.txt") }()
	var id string
	select {
	case id = <-approval:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if _, err := config.SetCredential("APPROVAL_SNAPSHOT_KEY", "after-save"); err != nil {
		t.Fatal(err)
	}
	applied, desired, err := ctrl.ModelSettingsState()
	if err != nil || applied == desired {
		t.Fatal("save did not mark the pending approval runtime stale")
	}
	ctrl.Approve(id, true, false, false)
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	for range 2 {
		select {
		case key := <-keys:
			if key != "Bearer before-save" {
				t.Fatal("approval resume crossed credential generations")
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	content, err := os.ReadFile(filepath.Join(root, "approved.txt"))
	if err != nil || string(content) != "approved snapshot" {
		t.Fatalf("approved tool did not finish: %v", err)
	}
}
