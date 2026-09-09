package bot

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"time"

	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/history"
	"reasonix/internal/provider"
	"reasonix/internal/stats"
)

func TestBotNewRunAppliesModelSettingsAndKeepsSessionOnFailure(t *testing.T) {
	closeCatalogs := func() {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := history.CloseSharedCatalog(ctx); err != nil {
			t.Fatalf("close shared history catalog: %v", err)
		}
		if err := stats.CloseUsageCatalogs(ctx); err != nil {
			t.Fatalf("close usage catalogs: %v", err)
		}
	}
	closeCatalogs()
	t.Setenv("REASONIX_HOME", t.TempDir())
	root := t.TempDir()
	// These projections belong to the process, not an individual controller.
	// Release SQLite handles before the isolated home is removed on Windows.
	t.Cleanup(closeCatalogs)
	cfg := config.Default()
	cfg.Providers = []config.ProviderEntry{{Name: "snapshot", Kind: "openai", BaseURL: "http://127.0.0.1:1/v1", Model: "m", APIKeyEnv: "BOT_SNAPSHOT_TEST_KEY"}}
	cfg.DefaultModel = "snapshot/m"
	if _, err := config.SetCredential("BOT_SNAPSHOT_TEST_KEY", "test-key"); err != nil {
		t.Fatal(err)
	}
	save := func() {
		t.Helper()
		if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
			t.Fatal(err)
		}
	}
	save()
	gw := NewGateway(GatewayConfig{Model: cfg.DefaultModel, WorkspaceRoot: root}, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	msg := InboundMessage{Platform: PlatformFeishu, ChatType: ChatDM, ChatID: "snapshot-test", UserID: "test"}
	key := BuildSessionKey(msg.Session())
	built, err := gw.buildSessionState(context.Background(), key, msg, gw.sessionProfileForMessage(msg), nil)
	if err != nil {
		t.Fatal(err)
	}
	old := built.state
	gw.controllers[key] = old
	t.Cleanup(func() { gw.closeSessionState(gw.controllers[key]) })
	oldCtrl := old.ctrl
	path := oldCtrl.SessionPath()
	oldConcrete := built.state.ctrl
	// Carry a real transcript across the same lease and runtime replacement.
	oldConcrete.(*control.Controller).AdoptHistory(append(oldConcrete.(*control.Controller).History(), provider.Message{Role: provider.RoleUser, Content: "preserved bot history"}), path)
	oldLease := old.leases
	cfg.Agent.PlannerModel = "missing/model"
	save()
	if _, err := gw.applySessionModelSettings(context.Background(), key, msg, old); err == nil {
		t.Fatal("invalid saved planner admitted a new bot run")
	}
	if gw.controllers[key] != old || old.ctrl != oldCtrl || old.leases != oldLease || old.retired {
		t.Fatal("failed application replaced the original runtime or lease")
	}
	cfg.Agent.PlannerModel = ""
	cfg.Agent.SubagentModel = "snapshot/m"
	save()
	next, err := gw.applySessionModelSettings(context.Background(), key, msg, old)
	if err != nil {
		t.Fatal(err)
	}
	if next == old || next.ctrl == oldCtrl || next.ctrl.SessionPath() != path || next.leases != oldLease {
		t.Fatal("new snapshot did not preserve the session binding")
	}
	history := next.ctrl.(*control.Controller).History()
	if history[len(history)-1].Content != "preserved bot history" {
		t.Fatal("new snapshot lost the transcript")
	}
	same, err := gw.applySessionModelSettings(context.Background(), key, msg, next)
	if err != nil || same != next {
		t.Fatalf("unchanged settings caused another rebuild: %v", err)
	}
}
