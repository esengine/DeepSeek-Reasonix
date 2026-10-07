package serve

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/session/control"
	"reasonix/internal/state/sessionstore"
)

type failingForkSession struct {
	control.SessionAPI
	path string
	err  error
}

func (s failingForkSession) ForkTurn(string, int, int, string) (string, error) {
	return s.path, s.err
}

func TestHubForkErrorsKeepInternalDetailsOutOfResponse(t *testing.T) {
	for _, scenario := range []struct {
		name    string
		code    string
		status  int
		message string
	}{
		{"save", "fork.failed", http.StatusInternalServerError, "could not create the conversation fork"},
		{"busy", "fork.busy", http.StatusConflict, "wait for the running turn to finish before forking"},
		{"open", "fork.open_failed", http.StatusInternalServerError, "the fork was created, but cannot be opened right now; please try again"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			var logs bytes.Buffer
			previous := slog.Default()
			slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
			t.Cleanup(func() { slog.SetDefault(previous) })
			root := testenv.TempDir(t)
			internalPath := filepath.Join(root, "INTERNAL_ERROR_DETAILS", "private.json")
			underlying := &os.PathError{Op: "open", Path: internalPath, Err: os.ErrPermission}
			ctrl := control.New(control.Options{WorkspaceRoot: root, Sink: event.Discard})
			failing := failingForkSession{SessionAPI: ctrl, err: underlying}
			if scenario.name == "busy" {
				failing.err = fmt.Errorf("%w: %w", control.ErrForkBusy, underlying)
			}
			savedPath := filepath.Join(root, "saved.jsonl")
			if scenario.name == "open" {
				saved := sessionstore.NewSession("sys")
				saved.Add(provider.Message{Role: provider.RoleUser, Content: "question"})
				saved.Add(provider.Message{Role: provider.RoleAssistant, Content: "answer"})
				if err := saved.SaveIfAbsent(savedPath); err != nil {
					t.Fatal(err)
				}
				failing.path, failing.err = savedPath, nil
			}
			hub := NewHub(HubOptions{})
			defer hub.Shutdown()
			bc := NewBroadcaster()
			rt, err := hub.Adopt(New(failing, bc, config.ServeConfig{}), bc)
			if err != nil {
				t.Fatal(err)
			}
			if scenario.name == "open" {
				rt.Root = filepath.Join(root, "INTERNAL_ERROR_DETAILS", "missing-workspace")
			}
			body, _ := json.Marshal(map[string]any{"sessionPath": "source", "turn": 0, "msgIndex": 1, "stamp": "stamp"})
			req := httptest.NewRequest(http.MethodPost, "/runtimes/"+rt.ID+"/fork", bytes.NewReader(body))
			req.SetPathValue("id", rt.ID)
			response := httptest.NewRecorder()
			hub.forkRuntime(response, req)
			var reason Reason
			if err := json.Unmarshal(response.Body.Bytes(), &reason); err != nil {
				t.Fatal(err)
			}
			if response.Code != scenario.status || reason.Code != scenario.code || reason.Message != scenario.message || len(reason.Params) != 0 {
				t.Fatalf("unexpected refusal: status=%d reason=%+v", response.Code, reason)
			}
			if strings.Contains(response.Body.String(), "INTERNAL_ERROR_DETAILS") || strings.Contains(response.Body.String(), "permission denied") || strings.Contains(response.Body.String(), root) || strings.Contains(response.Body.String(), "sessionPath") {
				t.Fatalf("response leaked internal details: %s", response.Body.String())
			}
			if strings.Contains(logs.String(), "INTERNAL_ERROR_DETAILS") || strings.Contains(logs.String(), root) || strings.Contains(logs.String(), "permission denied") {
				t.Fatalf("fork log leaked internal details: %s", logs.String())
			}
			if scenario.name != "busy" && !strings.Contains(logs.String(), "code="+scenario.code) {
				t.Fatalf("fork log omitted refusal code: %s", logs.String())
			}
			if scenario.name == "open" {
				if _, err := os.Stat(savedPath); err != nil {
					t.Fatalf("opening failure removed the saved fork: %v", err)
				}
			}
		})
	}
}
