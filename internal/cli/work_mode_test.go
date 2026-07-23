package cli

import (
	"errors"
	"strings"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/boot"
	"reasonix/internal/command"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/i18n"
	"reasonix/internal/provider"
	"reasonix/internal/skill"
)

func TestRuntimeProfileDisplayLocalizesLabels(t *testing.T) {
	defer i18n.DetectLanguage("en")
	for _, tt := range []struct {
		lang                        string
		economy, balanced, delivery string
	}{
		{lang: "en", economy: "economy", balanced: "balanced", delivery: "delivery"},
		{lang: "zh", economy: "轻量", balanced: "均衡", delivery: "交付"},
		{lang: "zh-TW", economy: "輕量", balanced: "均衡", delivery: "交付"},
	} {
		t.Run(tt.lang, func(t *testing.T) {
			i18n.DetectLanguage(tt.lang)
			for profile, want := range map[string]string{
				boot.TokenModeEconomy:  tt.economy,
				boot.TokenModeFull:     tt.balanced,
				"balanced":             tt.balanced,
				boot.TokenModeDelivery: tt.delivery,
			} {
				if got := runtimeProfileDisplay(profile); got != want {
					t.Errorf("runtimeProfileDisplay(%q) = %q, want %q", profile, got, want)
				}
			}
		})
	}
}

func TestRenderWorkModesShowsAllOptionsAndCurrent(t *testing.T) {
	out := renderWorkModes(100, boot.TokenModeFull)
	for _, want := range []string{"economy", "balanced", "delivery", "current"} {
		if !strings.Contains(out, want) {
			t.Fatalf("renderWorkModes missing %q:\n%s", want, out)
		}
	}
}

func TestParseWorkModeKeepsBalancedAsFullInternally(t *testing.T) {
	for input, want := range map[string]string{
		"economy":  boot.TokenModeEconomy,
		"balanced": boot.TokenModeFull,
		"full":     boot.TokenModeFull,
		"delivery": boot.TokenModeDelivery,
	} {
		got, ok := parseWorkMode(input)
		if !ok || got != want {
			t.Errorf("parseWorkMode(%q) = %q, %v; want %q, true", input, got, ok, want)
		}
	}
}

func TestWorkModeCompletionPublishesPrimaryCommandAndAliasArguments(t *testing.T) {
	m := newTestChatTUI()
	m.runtimeProfile = boot.TokenModeFull
	if !hasLabel(m.slashItems(), "/work-mode") {
		t.Fatal("slash completion missing /work-mode")
	}
	if hasLabel(m.slashItems(), "/profile") {
		t.Fatal("technical /profile alias should not duplicate the primary command in the slash menu")
	}
	for _, input := range []string{"/work-mode ", "/profile "} {
		items, _, ok := m.slashArgItems(input)
		if !ok {
			t.Fatalf("%q did not activate work-mode argument completion", input)
		}
		for _, want := range []string{"economy", "balanced", "delivery"} {
			if !hasLabel(items, want) {
				t.Errorf("%q completion missing %q: %v", input, want, labels(items))
			}
		}
	}
}

func TestWorkModeHelpAndStatusUseUserFacingName(t *testing.T) {
	if !hasLabel(builtinHelpItems(), "/work-mode") {
		t.Fatal("built-in help missing /work-mode")
	}
	if hasLabel(builtinHelpItems(), "/profile") {
		t.Fatal("built-in help should not duplicate the technical /profile alias")
	}
	m := newChatTUI(control.New(control.Options{Label: "model"}), "", make(chan event.Event, 1), 80)
	m.runtimeProfile = boot.TokenModeDelivery
	if got := m.workModeTag(); !strings.Contains(got, "delivery") {
		t.Fatalf("work-mode status tag = %q, want delivery", got)
	}
	m.width = 30
	if got := m.computeStatusLineCount(m.width); got < 2 {
		t.Fatalf("status-line count with work-mode tag = %d, want at least 2", got)
	}
}

func TestWorkModeSwitchBuildsTargetProfileAndSwapsAtomically(t *testing.T) {
	oldCtrl := control.New(control.Options{Label: "old"})
	oldCtrl.SetToolApprovalMode(control.ToolApprovalAuto)
	oldCtrl.SetPlanMode(true)
	newCtrl := control.New(control.Options{
		Label:    "new",
		Commands: []command.Command{{Name: "fresh-command"}},
		Skills:   []skill.Skill{{Name: "Fresh Skill"}},
	})
	m := newChatTUI(oldCtrl, "", make(chan event.Event, 1), 100)
	m.modelRef = "provider/model"
	m.runtimeProfile = boot.TokenModeFull
	var gotSpec controllerBuildSpec
	var gotCarry []provider.Message
	m.buildController = func(spec controllerBuildSpec, carry []provider.Message, _ string, _ control.SessionAPI) (*control.Controller, error) {
		gotSpec = spec
		gotCarry = carry
		return newCtrl, nil
	}

	cmd := m.runWorkModeCommand("/work-mode delivery")
	if cmd == nil {
		t.Fatal("work-mode switch did not schedule a controller build")
	}
	if m.ctrl != oldCtrl || m.runtimeProfile != boot.TokenModeFull {
		t.Fatal("controller or profile changed before the replacement build completed")
	}
	next, _ := m.Update(cmd())
	m = next.(chatTUI)

	if gotSpec.ModelRef != "provider/model" || gotSpec.RuntimeProfile != boot.TokenModeDelivery {
		t.Fatalf("build spec = %+v, want current model and delivery profile", gotSpec)
	}
	if gotSpec.ToolApprovalMode != control.ToolApprovalAuto {
		t.Fatalf("build spec approval mode = %q, want auto", gotSpec.ToolApprovalMode)
	}
	if !gotSpec.PlanMode {
		t.Fatal("build spec did not preserve plan mode")
	}
	if len(gotCarry) != len(oldCtrl.History()) {
		t.Fatalf("carried history length = %d, want %d", len(gotCarry), len(oldCtrl.History()))
	}
	if m.ctrl != newCtrl || m.runtimeProfile != boot.TokenModeDelivery {
		t.Fatalf("successful switch did not install replacement controller/profile: ctrl=%p profile=%q", m.ctrl, m.runtimeProfile)
	}
	if m.label != newCtrl.Label() || len(m.commands) != 1 || len(m.skills) != 1 || m.host != newCtrl.Host() {
		t.Fatalf("successful switch did not refresh controller metadata: label=%q commands=%d skills=%d", m.label, len(m.commands), len(m.skills))
	}
	if len(m.oldControllers) != 1 || m.oldControllers[0] != oldCtrl {
		t.Fatal("successful switch did not retain the old controller for exit-time cleanup")
	}
}

func TestWorkModeSwitchFailureKeepsOldControllerAndProfile(t *testing.T) {
	session := agent.NewSession("system")
	session.Add(provider.Message{Role: provider.RoleUser, Content: "keep this history"})
	exec := agent.New(nil, nil, session, agent.Options{}, event.Discard)
	oldCtrl := control.New(control.Options{Label: "old", Executor: exec})
	m := newChatTUI(oldCtrl, "", make(chan event.Event, 1), 100)
	m.modelRef = "provider/model"
	m.runtimeProfile = boot.TokenModeEconomy
	m.buildController = func(controllerBuildSpec, []provider.Message, string, control.SessionAPI) (*control.Controller, error) {
		return nil, errors.New("build failed")
	}

	cmd := m.runSlashCommand("/profile delivery")
	if cmd == nil {
		t.Fatal("/profile alias did not schedule a controller build")
	}
	next, _ := m.Update(cmd())
	m = next.(chatTUI)

	if m.ctrl != oldCtrl || m.runtimeProfile != boot.TokenModeEconomy {
		t.Fatalf("failed switch changed live runtime: ctrl=%p profile=%q", m.ctrl, m.runtimeProfile)
	}
	if history := m.ctrl.History(); len(history) != 2 || history[1].Content != "keep this history" {
		t.Fatalf("failed switch lost history: %#v", history)
	}
	if m.modelSwitchPending || m.pendingModelSwitch != nil {
		t.Fatal("failed switch left the runtime-switch gate pending")
	}
	if len(m.oldControllers) != 0 {
		t.Fatal("failed switch retired the still-live old controller")
	}
}

func TestWorkModeSwitchRejectsInvalidSameAndBusyRequests(t *testing.T) {
	m := newTestChatTUI()
	m.ctrl = control.New(control.Options{Label: "model"})
	m.modelRef = "provider/model"
	m.runtimeProfile = boot.TokenModeFull
	builds := 0
	m.buildController = func(controllerBuildSpec, []provider.Message, string, control.SessionAPI) (*control.Controller, error) {
		builds++
		return control.New(control.Options{Label: "new"}), nil
	}

	for _, input := range []string{
		"/work-mode unknown",
		"/work-mode balanced",
		"/work-mode economy extra",
	} {
		if cmd := m.runWorkModeCommand(input); cmd != nil {
			t.Fatalf("%q unexpectedly scheduled a build", input)
		}
	}
	m.pendingApproval = &event.Approval{ID: "approval", Tool: "bash"}
	if cmd := m.runWorkModeCommand("/work-mode economy"); cmd != nil {
		t.Fatal("pending approval should block work-mode switching")
	}
	if builds != 0 {
		t.Fatalf("rejected work-mode requests triggered %d builds", builds)
	}
}

func TestWorkModeSwitchRejectsRunningTurn(t *testing.T) {
	runner := &blockingTurnRunner{started: make(chan struct{})}
	ctrl := control.New(control.Options{Runner: runner, Sink: event.Discard, SessionDir: t.TempDir(), Label: "model"})
	ctrl.Send("keep running")
	<-runner.started
	t.Cleanup(func() {
		ctrl.Cancel()
		deadline := time.Now().Add(2 * time.Second)
		for ctrl.Running() && time.Now().Before(deadline) {
			time.Sleep(time.Millisecond)
		}
	})

	m := newTestChatTUI()
	m.ctrl = ctrl
	m.modelRef = "provider/model"
	m.runtimeProfile = boot.TokenModeFull
	builds := 0
	m.buildController = func(controllerBuildSpec, []provider.Message, string, control.SessionAPI) (*control.Controller, error) {
		builds++
		return control.New(control.Options{}), nil
	}
	if cmd := m.runWorkModeCommand("/work-mode delivery"); cmd != nil {
		t.Fatal("running turn should block work-mode switching")
	}
	if builds != 0 {
		t.Fatalf("running-turn guard triggered %d builds", builds)
	}
}

func TestRuntimeRebuildCommandsCarryCurrentWorkMode(t *testing.T) {
	isolateUserConfig(t)
	m := newTestChatTUI()
	m.ctrl = control.New(control.Options{Label: "deepseek-flash"})
	m.modelRef = "deepseek-flash/deepseek-v4-flash"
	m.runtimeProfile = boot.TokenModeDelivery
	var specs []controllerBuildSpec
	m.buildController = func(spec controllerBuildSpec, _ []provider.Message, _ string, _ control.SessionAPI) (*control.Controller, error) {
		specs = append(specs, spec)
		return control.New(control.Options{Label: "deepseek-flash"}), nil
	}

	cmd := m.runEffortCommand("/effort max")
	if cmd == nil {
		t.Fatal("effort switch did not schedule a rebuild")
	}
	next, _ := m.Update(cmd())
	m = next.(chatTUI)

	m.runModelSubcommand("/model deepseek-flash/another-model")
	if m.pendingModelSwitch == nil {
		t.Fatal("model switch did not schedule a rebuild")
	}
	next, _ = m.Update(m.pendingModelSwitch())
	m = next.(chatTUI)

	if !m.scheduleSkillSessionRefresh("test", "") {
		t.Fatal("skill refresh did not schedule a rebuild")
	}
	next, _ = m.Update(m.pendingModelSwitch())
	m = next.(chatTUI)

	if len(specs) != 3 {
		t.Fatalf("runtime rebuild count = %d, want 3", len(specs))
	}
	if specs[0].EffortOverride == nil || *specs[0].EffortOverride != "max" {
		t.Fatalf("effort rebuild override = %v, want max", specs[0].EffortOverride)
	}
	if specs[1].EffortOverride != nil || specs[2].EffortOverride != nil {
		t.Fatalf("non-effort rebuilds unexpectedly replaced effort override: %+v", specs)
	}
	for i, spec := range specs {
		if spec.RuntimeProfile != boot.TokenModeDelivery {
			t.Errorf("rebuild %d lost delivery profile: %+v", i, spec)
		}
	}
}
