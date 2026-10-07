package serve

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/session/control"
	"reasonix/internal/state/checkpoint"
	"reasonix/internal/state/sessionstore"
)

func TestHubForkOpensTheCompletedPrefixInAnotherRuntime(t *testing.T) {
	writeOpenableConfig(t)
	home, err := filepath.EvalSymlinks(os.Getenv("REASONIX_HOME"))
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("REASONIX_HOME", home)
	root := testenv.TempDir(t)
	ag := agent.New(&turnIdentityProvider{}, tool.NewRegistry(), sessionstore.NewSession("sys"), agent.Options{}, event.Discard)
	ctrl := control.New(control.Options{Runner: ag, Executor: ag, WorkspaceRoot: root, ModelRef: "default/shared-chat",
		SessionDir: SessionDirFor(root), SessionPath: filepath.Join(SessionDirFor(root), "source.jsonl")})
	h := NewHub(HubOptions{})
	defer h.Shutdown()
	bc := NewBroadcaster()
	rt, err := h.Adopt(New(ctrl, bc, config.ServeConfig{}), bc)
	if err != nil {
		t.Fatal(err)
	}
	for _, prompt := range []string{"first", "second"} {
		if err := ctrl.Run(context.Background(), prompt); err != nil {
			t.Fatal(err)
		}
	}
	cp := ctrl.Checkpoints()[0]
	parent := ctrl.SessionPath()
	if err := sessionstore.RenameSession(parent, "Source custom title"); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{"sessionPath": parent, "turn": cp.Turn,
		"msgIndex": cp.MsgIndex, "stamp": cp.Time.Format(time.RFC3339Nano)})
	srv := httptest.NewServer(operatorHandler(h))
	defer srv.Close()
	res, err := http.Post(srv.URL+"/runtimes/"+rt.ID+"/fork", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		t.Fatalf("fork status %d", res.StatusCode)
	}
	var child RuntimeView
	if err := json.NewDecoder(res.Body).Decode(&child); err != nil {
		t.Fatal(err)
	}
	if child.ID == rt.ID || child.Root != root || ctrl.SessionPath() != parent || len(h.List()) != 2 {
		t.Fatalf("fork did not open separately: %+v", child)
	}
	meta, _, err := sessionstore.LoadBranchMeta(child.SessionPath)
	if err != nil {
		t.Fatal(err)
	}
	if meta.CustomTitle != "" {
		t.Fatalf("fork bypassed normal naming with a forced title: %q", meta.CustomTitle)
	}
	msgs := h.Get(child.ID).Server.Controller().History()
	if msgs[len(msgs)-1].Content != "answered" || len(msgs) >= len(ctrl.History()) {
		t.Fatalf("wrong child history: %+v", msgs)
	}

	childServer := h.Get(child.ID).Server
	var checkpoints []struct {
		Turn     int `json:"turn"`
		MsgIndex int `json:"msgIndex"`
		Files    int `json:"files"`
	}
	decodeGet(t, childServer.checkpoints, "/checkpoints", &checkpoints)
	if len(checkpoints) != 1 || checkpoints[0].Turn != cp.Turn || checkpoints[0].MsgIndex != cp.MsgIndex || checkpoints[0].Files != 0 {
		t.Fatalf("fork checkpoint response: %+v", checkpoints)
	}
	request, _ := json.Marshal(map[string]any{"turn": cp.Turn, "scope": "conversation"})
	prepared := httptest.NewRecorder()
	childServer.rewindPrepare(prepared, httptest.NewRequest(http.MethodPost, "/rewind/prepare", bytes.NewReader(request)))
	var plan checkpoint.RewindPlan
	if err := json.Unmarshal(prepared.Body.Bytes(), &plan); err != nil {
		t.Fatal(err)
	}
	if prepared.Code != http.StatusOK || !plan.CanConversation {
		t.Fatalf("fork rewind preparation: %s", prepared.Body.String())
	}
	request, _ = json.Marshal(map[string]any{"planId": plan.PlanID})
	committed := httptest.NewRecorder()
	childServer.rewindCommit(committed, httptest.NewRequest(http.MethodPost, "/rewind/commit", bytes.NewReader(request)))
	if committed.Code != http.StatusOK || len(childServer.Controller().History()) != cp.MsgIndex {
		t.Fatalf("fork rewind commit: %s", committed.Body.String())
	}
	if len(ctrl.Checkpoints()) != 2 || ctrl.HistoryLen() <= len(msgs) {
		t.Fatal("fork rewind changed the source")
	}
	request, _ = json.Marshal(map[string]any{"path": child.SessionPath, "title": "Source custom title (branch)"})
	renamed, err := http.Post(srv.URL+"/tree/sessions/rename", "application/json", bytes.NewReader(request))
	if err != nil {
		t.Fatal(err)
	}
	defer renamed.Body.Close()
	childMeta, _, err := sessionstore.LoadBranchMeta(child.SessionPath)
	if err != nil {
		t.Fatal(err)
	}
	parentMeta, _, err := sessionstore.LoadBranchMeta(parent)
	if err != nil {
		t.Fatal(err)
	}
	if renamed.StatusCode != http.StatusNoContent || childMeta.CustomTitle != "Source custom title (branch)" || parentMeta.CustomTitle != "Source custom title" {
		t.Fatalf("localized fork rename: status=%d child=%q parent=%q", renamed.StatusCode, childMeta.CustomTitle, parentMeta.CustomTitle)
	}
}
