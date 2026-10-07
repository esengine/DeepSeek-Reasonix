package serve

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/provider"
	"reasonix/internal/state/sessionstore"
	"reasonix/internal/state/store"
)

type forkSettingsResolver struct {
	*provider.StaticResolver
	beforeResolve func() error
}

func (r *forkSettingsResolver) Resolve(selection provider.Selection) (provider.Provider, error) {
	if r.beforeResolve != nil {
		if err := r.beforeResolve(); err != nil {
			return nil, err
		}
	}
	return r.StaticResolver.Resolve(selection)
}

func TestHubForkInheritsLiveSettingsAfterDurableSave(t *testing.T) {
	writeServeModelConfig(t)
	closeSharedCatalogsOnCleanup(t)
	resolver := &forkSettingsResolver{StaticResolver: homeResolver()}
	resolver.Providers["home/chat-a"] = &turnIdentityProvider{}
	h := NewHub(HubOptions{ProviderResolver: resolver})
	defer h.Shutdown()
	root := testenv.TempDir(t)
	sourcePath := sessionstore.NewSessionPath(SessionDirFor(root), "source")
	if err := sessionstore.NewSession("sys").SaveIfAbsent(sourcePath); err != nil {
		t.Fatal(err)
	}
	source, err := h.Open(context.Background(), OpenRequest{Root: root, Model: "home/chat-a", SessionPath: sourcePath})
	if err != nil {
		t.Fatal(err)
	}
	ctrl := source.Server.Controller()
	if err := ctrl.Run(context.Background(), "first"); err != nil {
		t.Fatal(err)
	}
	ctrl.SetAgentPreset("delivery")
	ctrl.SetPlanMode(true)
	parent := ctrl.SessionPath()
	oldMeta, _, err := sessionstore.LoadBranchMeta(parent)
	if err != nil {
		t.Fatal(err)
	}
	oldMeta.AgentPreset, oldMeta.Mode = "balanced", "agent"
	if err := sessionstore.SaveBranchMeta(parent, oldMeta); err != nil {
		t.Fatal(err)
	}
	cp := ctrl.Checkpoints()[0]
	var prepared atomic.Bool
	resolver.beforeResolve = func() error {
		if len(h.List()) != 1 {
			return fmt.Errorf("child runtime published before construction finished")
		}
		sessions, err := sessionstore.ListSessions(filepath.Dir(parent))
		if err != nil {
			return err
		}
		for _, session := range sessions {
			if session.Path == parent {
				continue
			}
			meta, ok, err := sessionstore.LoadBranchMeta(session.Path)
			if err != nil {
				return err
			}
			if !ok || meta.ParentID != sessionstore.BranchID(parent) {
				continue
			}
			if meta.Model != "home/chat-a" || meta.AgentPreset != "delivery" || meta.Mode != "plan" || sessionstore.IsCleanupPending(session.Path) {
				return fmt.Errorf("fork settings not durably committed before provider creation: %+v", meta)
			}
			if _, err := os.Stat(filepath.Join(store.SessionCheckpointDir(session.Path), fmt.Sprintf("turn-%d.json", cp.Turn))); err != nil {
				return err
			}
			prepared.Store(true)
			return nil
		}
		return fmt.Errorf("fork is not complete before provider creation")
	}
	body, _ := json.Marshal(map[string]any{"sessionPath": parent, "turn": cp.Turn, "msgIndex": cp.MsgIndex, "stamp": cp.Time.Format(time.RFC3339Nano)})
	srv := httptest.NewServer(operatorHandler(h))
	defer srv.Close()
	response, err := http.Post(srv.URL+"/runtimes/"+source.ID+"/fork", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	var view RuntimeView
	if err := json.NewDecoder(response.Body).Decode(&view); err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != http.StatusOK || !prepared.Load() {
		t.Fatalf("fork was not prepared before opening: status=%d", response.StatusCode)
	}
	child := h.Get(view.ID).Server.Controller()
	meta, _, err := sessionstore.LoadBranchMeta(view.SessionPath)
	if err != nil {
		t.Fatal(err)
	}
	if child == ctrl || child.ModelRef() != "home/chat-a" || child.AgentPreset() != "delivery" || !child.PlanMode() || meta.Model != child.ModelRef() || meta.AgentPreset != child.AgentPreset() || meta.Mode != "plan" {
		t.Fatalf("metadata/runtime mismatch: meta=%+v model=%s preset=%s plan=%v", meta, child.ModelRef(), child.AgentPreset(), child.PlanMode())
	}
	child.SetPlanMode(false)
	child.SetAgentPreset("balanced")
	if !ctrl.PlanMode() || ctrl.AgentPreset() != "delivery" {
		t.Fatal("child reused source runtime state")
	}
	child.SetAgentPreset("delivery")
	child.SetPlanMode(true)
}
