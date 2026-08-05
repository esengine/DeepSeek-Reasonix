package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/boot"
	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/evidence"
	"reasonix/internal/store"
)

func newGoalDeliveryYoloTestApp(t *testing.T, goalStatus string) (*App, *WorkspaceTab, control.SessionAPI, string) {
	t.Helper()
	isolateDesktopUserDirs(t)
	setDesktopTestCredential(t, "GOAL_DELIVERY_KEY", "sk-test")
	setDesktopTestCredential(t, "GOAL_DELIVERY_ALT_KEY", "sk-test")
	cfg := config.Default()
	cfg.DefaultModel = "test/model"
	cfg.Desktop.ProviderAccess = []string{"test", "alt"}
	cfg.Providers = []config.ProviderEntry{
		{
			Name: "test", Kind: "openai", BaseURL: "https://example.invalid/v1", Model: "model", APIKeyEnv: "GOAL_DELIVERY_KEY",
			SupportedEfforts: []string{"low", "high"},
		},
		{Name: "alt", Kind: "openai", BaseURL: "https://example.invalid/v1", Model: "alt-model", APIKeyEnv: "GOAL_DELIVERY_ALT_KEY"},
	}
	if err := cfg.SaveTo(config.UserConfigPath()); err != nil {
		t.Fatalf("save config: %v", err)
	}

	dir := config.SessionDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir session dir: %v", err)
	}
	path := filepath.Join(dir, "goal-delivery-yolo.jsonl")
	writeHistoryTestSession(t, path, "continue the delivery")
	checkpoint := evidence.DeliveryCheckpoint{
		ScopeID:             "goal-test-scope",
		CriteriaEstablished: true,
		WorkObserved:        true,
		MutationObserved:    true,
		PendingMutation:     true,
	}
	state := map[string]any{
		"goal":               "ship the combined mode",
		"status":             goalStatus,
		"researchMode":       control.GoalResearchOn,
		"autoResearchTaskID": "research-task-1",
		"scopeID":            checkpoint.ScopeID,
		"deliveryCheckpoint": checkpoint,
	}
	data, err := json.Marshal(state)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.SessionGoalState(path), data, 0o600); err != nil {
		t.Fatalf("write Goal sidecar: %v", err)
	}

	loaded, err := agent.LoadSession(path)
	if err != nil {
		t.Fatalf("load session: %v", err)
	}
	exec := agent.New(nil, nil, loaded, agent.Options{}, event.Discard)
	oldCtrl := control.New(control.Options{Executor: exec, SessionDir: dir, SessionPath: path, Label: "test/model", Sink: event.Discard})
	oldCtrl.Resume(loaded, path)
	oldCtrl.SetToolApprovalMode(control.ToolApprovalYolo)

	app := NewApp()
	app.ctx = context.Background()
	app.readyHook = func() {}
	tab := &WorkspaceTab{
		ID: "tab_goal_delivery_yolo", Scope: "global", Ready: true,
		SessionPath: path, model: "test/model", tokenMode: boot.TokenModeFull,
		mode: "yolo", toolApprovalMode: control.ToolApprovalYolo,
		goal: "stale tab goal", Ctrl: oldCtrl,
		disabledMCP: map[string]ServerView{},
	}
	tab.sink = &tabEventSink{tabID: tab.ID, app: app}
	app.tabs = map[string]*WorkspaceTab{tab.ID: tab}
	app.tabOrder = []string{tab.ID}
	app.activeTabID = tab.ID
	if err := tab.ensureSessionLease(path); err != nil {
		t.Fatalf("ensure session lease: %v", err)
	}
	app.mu.Lock()
	app.newSessionRuntimeLocked(tab, sessionRuntimeKey(path))
	app.advanceSessionRuntimeEpochLocked(tab)
	app.mu.Unlock()
	t.Cleanup(func() {
		if ctrl := app.controllerForTab(tab); ctrl != nil {
			ctrl.Close()
		}
		tab.releaseSessionLease()
	})
	return app, tab, oldCtrl, path
}

func TestGoalDeliveryYoloTokenSwitchPreservesBlockedGoalCheckpoint(t *testing.T) {
	app, tab, oldCtrl, path := newGoalDeliveryYoloTestApp(t, control.GoalStatusBlocked)
	if err := app.SetTokenModeForTab(tab.ID, boot.TokenModeDelivery); err != nil {
		t.Fatalf("SetTokenModeForTab: %v", err)
	}
	ctrl := app.controllerForTab(tab)
	if ctrl == nil || ctrl == oldCtrl {
		t.Fatal("token-mode switch did not install a replacement controller")
	}
	if ctrl.GoalStatus() != control.GoalStatusBlocked || ctrl.Goal() != "ship the combined mode" {
		t.Fatalf("rebuilt Goal = (%q, %q), want blocked Goal", ctrl.Goal(), ctrl.GoalStatus())
	}
	if ctrl.ToolApprovalMode() != control.ToolApprovalYolo {
		t.Fatalf("tool approval = %q, want yolo", ctrl.ToolApprovalMode())
	}
	if currentTabTokenMode(tab) != boot.TokenModeDelivery {
		t.Fatalf("token mode = %q, want delivery", currentTabTokenMode(tab))
	}
	app.mu.RLock()
	runtimeForPath := app.runtimeBySessionKey[sessionRuntimeKey(path)]
	runtimeCount := len(app.runtimeBySessionKey)
	app.mu.RUnlock()
	if runtimeForPath == nil || runtimeForPath.Owner != tab || runtimeCount != 1 {
		t.Fatalf("runtime registry after Goal+Delivery switch = owner %p count %d, want tab %p count 1", runtimeForPath, runtimeCount, tab)
	}
	var persisted struct {
		DeliveryCheckpoint evidence.DeliveryCheckpoint `json:"deliveryCheckpoint"`
	}
	data, err := os.ReadFile(store.SessionGoalState(path))
	if err != nil {
		t.Fatalf("read Goal sidecar: %v", err)
	}
	if err := json.Unmarshal(data, &persisted); err != nil {
		t.Fatalf("decode Goal sidecar: %v", err)
	}
	if persisted.DeliveryCheckpoint.ScopeID != "goal-test-scope" || !persisted.DeliveryCheckpoint.PendingMutation {
		t.Fatalf("persisted checkpoint = %+v", persisted.DeliveryCheckpoint)
	}
	if !app.ResumeGoalForTab(tab.ID) || ctrl.GoalStatus() != control.GoalStatusRunning {
		t.Fatal("blocked Goal did not resume after controller rebuild")
	}
}

func TestGoalDeliveryYoloTokenSwitchDoesNotReviveCompletedGoal(t *testing.T) {
	app, tab, _, _ := newGoalDeliveryYoloTestApp(t, control.GoalStatusComplete)
	if err := app.SetTokenModeForTab(tab.ID, boot.TokenModeDelivery); err != nil {
		t.Fatalf("SetTokenModeForTab: %v", err)
	}
	ctrl := app.controllerForTab(tab)
	if ctrl.GoalStatus() == control.GoalStatusRunning {
		t.Fatalf("completed Goal was revived: goal=%q status=%q", ctrl.Goal(), ctrl.GoalStatus())
	}
	if app.ResumeGoalForTab(tab.ID) {
		t.Fatal("completed Goal should not be resumable")
	}
}

func TestPlanYoloDeliveryRebuildUsesLiveControllerAxes(t *testing.T) {
	app, tab, oldCtrl, _ := newGoalDeliveryYoloTestApp(t, control.GoalStatusBlocked)
	oldCtrl.SetPlanMode(true)
	oldCtrl.SetToolApprovalMode(control.ToolApprovalYolo)
	// Simulate stale tab metadata: the rebuild must snapshot the admitted
	// Controller state, not restore these lagging persistence fields.
	app.mu.Lock()
	tab.mode = "normal"
	tab.toolApprovalMode = ""
	app.mu.Unlock()

	if err := app.SetTokenModeForTab(tab.ID, boot.TokenModeDelivery); err != nil {
		t.Fatalf("SetTokenModeForTab: %v", err)
	}
	ctrl := app.controllerForTab(tab)
	if ctrl == nil || ctrl == oldCtrl {
		t.Fatal("token-mode switch did not install a replacement controller")
	}
	if !ctrl.PlanMode() || ctrl.ToolApprovalMode() != control.ToolApprovalYolo {
		t.Fatalf("rebuilt axes plan=%v approval=%q, want true/yolo", ctrl.PlanMode(), ctrl.ToolApprovalMode())
	}
	if ctrl.GoalStatus() != control.GoalStatusBlocked {
		t.Fatalf("blocked Goal status = %q, want preserved while Plan is active", ctrl.GoalStatus())
	}
	if currentTabTokenMode(tab) != boot.TokenModeDelivery {
		t.Fatalf("token mode = %q, want delivery", currentTabTokenMode(tab))
	}
}

func TestPlanWinsRunningGoalConflictDuringDeliveryRebuild(t *testing.T) {
	app, tab, oldCtrl, path := newGoalDeliveryYoloTestApp(t, control.GoalStatusRunning)
	oldCtrl.SetPlanMode(true)
	oldCtrl.SetToolApprovalMode(control.ToolApprovalYolo)
	app.mu.Lock()
	tab.mode = "plan-yolo"
	tab.goal = "ship the combined mode"
	app.mu.Unlock()

	if err := app.SetTokenModeForTab(tab.ID, boot.TokenModeDelivery); err != nil {
		t.Fatalf("SetTokenModeForTab: %v", err)
	}
	ctrl := app.controllerForTab(tab)
	if !ctrl.PlanMode() || ctrl.ToolApprovalMode() != control.ToolApprovalYolo {
		t.Fatalf("rebuilt axes plan=%v approval=%q, want true/yolo", ctrl.PlanMode(), ctrl.ToolApprovalMode())
	}
	if ctrl.GoalStatus() == control.GoalStatusRunning || strings.TrimSpace(ctrl.Goal()) != "" {
		t.Fatalf("Plan/Goal conflict survived rebuild: goal=%q status=%q", ctrl.Goal(), ctrl.GoalStatus())
	}
	var persisted struct {
		Goal   string `json:"goal"`
		Status string `json:"status"`
	}
	data, err := os.ReadFile(store.SessionGoalState(path))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &persisted); err != nil {
		t.Fatal(err)
	}
	if persisted.Status == control.GoalStatusRunning || strings.TrimSpace(persisted.Goal) != "" {
		t.Fatalf("conflicting Goal sidecar = %+v, want cleared", persisted)
	}
}

func TestRunningGoalDeliveryYoloRebuildKeepsScopeAndAutoResearch(t *testing.T) {
	app, tab, oldCtrl, path := newGoalDeliveryYoloTestApp(t, control.GoalStatusRunning)
	if err := app.SetTokenModeForTab(tab.ID, boot.TokenModeDelivery); err != nil {
		t.Fatalf("SetTokenModeForTab: %v", err)
	}
	ctrl := app.controllerForTab(tab)
	if ctrl == nil || ctrl == oldCtrl || ctrl.GoalStatus() != control.GoalStatusRunning {
		t.Fatalf("running Goal was not restored: ctrl=%T goal=%q status=%q", ctrl, ctrl.Goal(), ctrl.GoalStatus())
	}
	if ctrl.ToolApprovalMode() != control.ToolApprovalYolo {
		t.Fatalf("tool approval = %q, want yolo", ctrl.ToolApprovalMode())
	}
	var persisted struct {
		ScopeID            string                      `json:"scopeID"`
		AutoResearchTaskID string                      `json:"autoResearchTaskID"`
		DeliveryCheckpoint evidence.DeliveryCheckpoint `json:"deliveryCheckpoint"`
	}
	data, err := os.ReadFile(store.SessionGoalState(path))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &persisted); err != nil {
		t.Fatal(err)
	}
	if persisted.ScopeID != "goal-test-scope" || persisted.AutoResearchTaskID != "research-task-1" {
		t.Fatalf("restored Goal identity = %+v", persisted)
	}
	if persisted.DeliveryCheckpoint.ScopeID != persisted.ScopeID || !persisted.DeliveryCheckpoint.PendingMutation {
		t.Fatalf("restored Delivery checkpoint = %+v", persisted.DeliveryCheckpoint)
	}
}

func TestGoalDeliveryYoloSurvivesEveryControllerRebuildPath(t *testing.T) {
	for _, tc := range []struct {
		name    string
		prepare func(*App, *WorkspaceTab)
		rebuild func(*App, *WorkspaceTab) error
	}{
		{
			name: "settings",
			prepare: func(app *App, tab *WorkspaceTab) {
				app.mu.Lock()
				tab.tokenMode = boot.TokenModeDelivery
				app.mu.Unlock()
			},
			rebuild: func(app *App, _ *WorkspaceTab) error {
				return app.rebuildSetting("settings")
			},
		},
		{
			name: "model",
			prepare: func(app *App, tab *WorkspaceTab) {
				app.mu.Lock()
				tab.tokenMode = boot.TokenModeDelivery
				app.mu.Unlock()
			},
			rebuild: func(app *App, tab *WorkspaceTab) error {
				return app.SetModelForTab(tab.ID, "alt/alt-model")
			},
		},
		{
			name: "effort",
			prepare: func(app *App, tab *WorkspaceTab) {
				app.mu.Lock()
				tab.tokenMode = boot.TokenModeDelivery
				app.mu.Unlock()
			},
			rebuild: func(app *App, tab *WorkspaceTab) error {
				return app.SetEffortForTab(tab.ID, "high")
			},
		},
		{
			name:    "token mode",
			prepare: func(*App, *WorkspaceTab) {},
			rebuild: func(app *App, tab *WorkspaceTab) error {
				return app.SetTokenModeForTab(tab.ID, boot.TokenModeDelivery)
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app, tab, oldCtrl, path := newGoalDeliveryYoloTestApp(t, control.GoalStatusRunning)
			tc.prepare(app, tab)
			if err := tc.rebuild(app, tab); err != nil {
				t.Fatalf("rebuild: %v", err)
			}

			ctrl := app.controllerForTab(tab)
			if ctrl == nil || ctrl == oldCtrl {
				t.Fatal("rebuild did not install a replacement controller")
			}
			if ctrl.PlanMode() || ctrl.GoalStatus() != control.GoalStatusRunning || ctrl.Goal() != "ship the combined mode" {
				t.Fatalf("collaboration state plan=%v goal=%q status=%q, want running Goal", ctrl.PlanMode(), ctrl.Goal(), ctrl.GoalStatus())
			}
			if ctrl.ToolApprovalMode() != control.ToolApprovalYolo || currentTabTokenMode(tab) != boot.TokenModeDelivery {
				t.Fatalf("runtime axes approval=%q token=%q, want yolo/delivery", ctrl.ToolApprovalMode(), currentTabTokenMode(tab))
			}

			var persisted struct {
				ScopeID            string                      `json:"scopeID"`
				AutoResearchTaskID string                      `json:"autoResearchTaskID"`
				DeliveryCheckpoint evidence.DeliveryCheckpoint `json:"deliveryCheckpoint"`
			}
			data, err := os.ReadFile(store.SessionGoalState(path))
			if err != nil {
				t.Fatalf("read Goal sidecar: %v", err)
			}
			if err := json.Unmarshal(data, &persisted); err != nil {
				t.Fatalf("decode Goal sidecar: %v", err)
			}
			if persisted.ScopeID != "goal-test-scope" || persisted.AutoResearchTaskID != "research-task-1" {
				t.Fatalf("restored Goal identity = %+v", persisted)
			}
			if persisted.DeliveryCheckpoint.ScopeID != persisted.ScopeID || !persisted.DeliveryCheckpoint.PendingMutation {
				t.Fatalf("restored Delivery checkpoint = %+v", persisted.DeliveryCheckpoint)
			}
		})
	}
}

func TestPlanYoloDeliveryOrderConverges(t *testing.T) {
	for _, tc := range []struct {
		name string
		run  func(*App, *WorkspaceTab) error
	}{
		{
			name: "plan then yolo then delivery",
			run: func(app *App, tab *WorkspaceTab) error {
				app.SetCollaborationModeForTab(tab.ID, "plan")
				app.SetToolApprovalModeForTab(tab.ID, control.ToolApprovalYolo)
				return app.SetTokenModeForTab(tab.ID, boot.TokenModeDelivery)
			},
		},
		{
			name: "delivery then plan then yolo",
			run: func(app *App, tab *WorkspaceTab) error {
				if err := app.SetTokenModeForTab(tab.ID, boot.TokenModeDelivery); err != nil {
					return err
				}
				app.SetCollaborationModeForTab(tab.ID, "plan")
				app.SetToolApprovalModeForTab(tab.ID, control.ToolApprovalYolo)
				return nil
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			app, tab, _, _ := newGoalDeliveryYoloTestApp(t, control.GoalStatusComplete)
			app.SetToolApprovalModeForTab(tab.ID, control.ToolApprovalAsk)
			if err := tc.run(app, tab); err != nil {
				t.Fatal(err)
			}
			ctrl := app.controllerForTab(tab)
			if !ctrl.PlanMode() || ctrl.ToolApprovalMode() != control.ToolApprovalYolo || currentTabTokenMode(tab) != boot.TokenModeDelivery {
				t.Fatalf("final axes plan=%v approval=%q token=%q", ctrl.PlanMode(), ctrl.ToolApprovalMode(), currentTabTokenMode(tab))
			}
		})
	}
}

func TestEveryCollaborationAndTokenModeCombinationConvergesInBothOrders(t *testing.T) {
	collaborationModes := []string{"normal", "plan", "goal"}
	tokenModes := []string{boot.TokenModeEconomy, boot.TokenModeFull, boot.TokenModeDelivery}
	orders := []string{"collaboration-first", "token-first"}

	for _, collaborationMode := range collaborationModes {
		for _, tokenMode := range tokenModes {
			for _, order := range orders {
				t.Run(collaborationMode+"/"+tokenMode+"/"+order, func(t *testing.T) {
					app, tab, _, path := newGoalDeliveryYoloTestApp(t, control.GoalStatusRunning)
					app.SetToolApprovalModeForTab(tab.ID, control.ToolApprovalAsk)

					setCollaboration := func() {
						switch collaborationMode {
						case "goal":
							app.SetGoalForTab(tab.ID, "exercise all runtime axes")
							app.SetCollaborationModeForTab(tab.ID, "goal")
						case "plan":
							app.SetCollaborationModeForTab(tab.ID, "plan")
						default:
							app.SetGoalForTab(tab.ID, "")
							app.SetCollaborationModeForTab(tab.ID, "normal")
						}
					}
					setToken := func() {
						if err := app.SetTokenModeForTab(tab.ID, tokenMode); err != nil {
							t.Fatalf("SetTokenModeForTab(%q): %v", tokenMode, err)
						}
					}
					if order == "collaboration-first" {
						setCollaboration()
						setToken()
					} else {
						setToken()
						setCollaboration()
					}

					ctrl := app.controllerForTab(tab)
					if ctrl == nil {
						t.Fatal("final controller is nil")
					}
					if got := currentTabTokenMode(tab); got != tokenMode {
						t.Fatalf("token mode = %q, want %q", got, tokenMode)
					}
					switch collaborationMode {
					case "goal":
						if ctrl.PlanMode() || ctrl.GoalStatus() != control.GoalStatusRunning ||
							ctrl.Goal() != "exercise all runtime axes" {
							t.Fatalf("Goal axes plan=%v goal=%q status=%q", ctrl.PlanMode(), ctrl.Goal(), ctrl.GoalStatus())
						}
					case "plan":
						if !ctrl.PlanMode() || ctrl.GoalStatus() == control.GoalStatusRunning {
							t.Fatalf("Plan axes plan=%v goal=%q status=%q", ctrl.PlanMode(), ctrl.Goal(), ctrl.GoalStatus())
						}
					default:
						if ctrl.PlanMode() || ctrl.GoalStatus() == control.GoalStatusRunning {
							t.Fatalf("Normal axes plan=%v goal=%q status=%q", ctrl.PlanMode(), ctrl.Goal(), ctrl.GoalStatus())
						}
					}
					app.mu.RLock()
					runtimeForPath := app.runtimeBySessionKey[sessionRuntimeKey(path)]
					runtimeCount := len(app.runtimeBySessionKey)
					view := app.sessionRuntimeViewLocked(tab)
					app.mu.RUnlock()
					if runtimeForPath == nil || runtimeForPath.Owner != tab || runtimeCount != 1 {
						t.Fatalf("runtime registry owner=%#v count=%d, want one runtime for tab", runtimeForPath, runtimeCount)
					}
					if view.Phase != sessionRuntimeReady || tab.sessionLeaseRuntimeKey() != sessionRuntimeKey(path) {
						t.Fatalf("runtime phase=%q lease=%q, want ready/%q", view.Phase, tab.sessionLeaseRuntimeKey(), sessionRuntimeKey(path))
					}
				})
			}
		}
	}
}

func TestGoalAndCollaborationResyncBeforeSendPreserveRunningDeliveryScope(t *testing.T) {
	app, tab, ctrl, path := newGoalDeliveryYoloTestApp(t, control.GoalStatusRunning)
	tab.goal = ctrl.Goal()
	app.SetCollaborationModeForTab(tab.ID, "goal")
	app.SetGoalForTab(tab.ID, ctrl.Goal())

	var persisted struct {
		ScopeID string `json:"scopeID"`
	}
	data, err := os.ReadFile(store.SessionGoalState(path))
	if err != nil {
		t.Fatalf("read Goal sidecar: %v", err)
	}
	if err := json.Unmarshal(data, &persisted); err != nil {
		t.Fatalf("decode Goal sidecar: %v", err)
	}
	if persisted.ScopeID != "goal-test-scope" {
		t.Fatalf("Goal resync replaced delivery scope: got %q", persisted.ScopeID)
	}
}

func TestTokenModeSwitchWaitsForForegroundTurnAdmission(t *testing.T) {
	app, tab, _, _ := newGoalDeliveryYoloTestApp(t, control.GoalStatusBlocked)
	tab.turnStartMu.Lock()
	done := make(chan error, 1)
	started := make(chan struct{})
	go func() {
		close(started)
		done <- app.SetTokenModeForTab(tab.ID, boot.TokenModeDelivery)
	}()
	<-started
	for app.runtimeRebuildMu.TryLock() {
		app.runtimeRebuildMu.Unlock()
		select {
		case err := <-done:
			t.Fatalf("token-mode switch returned before reaching the admission gate: %v", err)
		default:
			runtime.Gosched()
		}
	}
	select {
	case err := <-done:
		t.Fatalf("token-mode switch crossed the turn-admission gate: %v", err)
	default:
	}
	tab.turnStartMu.Unlock()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("SetTokenModeForTab after admission release: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("token-mode switch did not finish after turn admission released")
	}
}
