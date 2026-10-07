package serve

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/session/control"
	"reasonix/internal/state/sessionstore"
)

func TestHubForkStaleCheckpointReturnsRefreshError(t *testing.T) {
	root := testenv.TempDir(t)
	executor := agent.New(&turnIdentityProvider{}, tool.NewRegistry(), sessionstore.NewSession("sys"), agent.Options{}, event.Discard)
	ctrl := control.New(control.Options{Runner: executor, Executor: executor, WorkspaceRoot: root,
		SessionDir: root, SessionPath: filepath.Join(root, "source.jsonl")})
	hub := NewHub(HubOptions{})
	defer hub.Shutdown()
	broadcaster := NewBroadcaster()
	rt, err := hub.Adopt(New(ctrl, broadcaster, config.ServeConfig{}), broadcaster)
	if err != nil {
		t.Fatal(err)
	}
	if err := ctrl.Run(context.Background(), "finished turn"); err != nil {
		t.Fatal(err)
	}
	cp := ctrl.Checkpoints()[0]
	body, err := json.Marshal(map[string]any{"sessionPath": ctrl.SessionPath(), "turn": cp.Turn,
		"msgIndex": cp.MsgIndex, "stamp": "outdated-checkpoint"})
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/runtimes/"+rt.ID+"/fork", bytes.NewReader(body))
	req.SetPathValue("id", rt.ID)
	response := httptest.NewRecorder()
	hub.forkRuntime(response, req)
	var reason Reason
	if err := json.Unmarshal(response.Body.Bytes(), &reason); err != nil {
		t.Fatal(err)
	}
	if response.Code != http.StatusConflict || reason.Code != "fork.stale" ||
		reason.Message != "the selected conversation checkpoint is no longer valid; refresh and select the final reply again" {
		t.Fatalf("wrong stale refusal: status=%d reason=%+v", response.Code, reason)
	}
}
