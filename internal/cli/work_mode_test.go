package cli

import (
	"strings"
	"testing"
	"time"

	"reasonix/internal/boot"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/i18n"
	"reasonix/internal/provider"
)

func TestRuntimeProfileDisplayLocalizesLabels(t *testing.T) {
	defer i18n.DetectLanguage("en")
	for _, tt := range []struct {
		lang                        string
		economy, balanced, delivery string
	}{
		{lang: "en", economy: "light", balanced: "balanced", delivery: "delivery"},
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
				"light":                tt.economy,
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
	for _, want := range []string{"light", "balanced", "delivery", "current"} {
		if !strings.Contains(out, want) {
			t.Fatalf("renderWorkModes missing %q:\n%s", want, out)
		}
	}
}

func TestParseWorkModeKeepsBalancedAsFullInternally(t *testing.T) {
	for input, want := range map[string]string{
		"economy":  boot.TokenModeEconomy,
		"light":    boot.TokenModeEconomy,
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
	if !hasLabel(m.slashItems(), "/preset") {
		t.Fatal("slash completion missing /preset")
	}
	if hasLabel(m.slashItems(), "/profile") {
		t.Fatal("technical /profile alias should not duplicate the primary command in the slash menu")
	}
	for _, input := range []string{"/preset ", "/work-mode ", "/profile "} {
		items, _, ok := m.slashArgItems(input)
		if !ok {
			t.Fatalf("%q did not activate preset argument completion", input)
		}
		for _, want := range []string{"light", "balanced", "delivery"} {
			if !hasLabel(items, want) {
				t.Errorf("%q completion missing %q: %v", input, want, labels(items))
			}
		}
	}
}

func TestWorkModeHelpAndStatusUseUserFacingName(t *testing.T) {
	if !hasLabel(builtinHelpItems(), "/preset") {
		t.Fatal("built-in help missing /preset")
	}
	if hasLabel(builtinHelpItems(), "/profile") {
		t.Fatal("built-in help should not duplicate the technical /profile alias")
	}
	m := newChatTUI(control.New(control.Options{Label: "model"}), "", make(chan event.Event, 1), 80)
	m.runtimeProfile = boot.TokenModeDelivery
	if got := m.workModeTag(); !strings.Contains(got, "delivery") {
		t.Fatalf("role-setting status tag = %q, want delivery", got)
	}
	m.width = 30
	if got := m.computeStatusLineCount(m.width); got < 2 {
		t.Fatalf("status-line count with role-setting tag = %d, want at least 2", got)
	}
}

func TestWorkModeSwitchUpdatesInPlaceWithoutRebuild(t *testing.T) {
	oldCtrl := control.New(control.Options{Label: "old"})
	oldCtrl.SetToolApprovalMode(control.ToolApprovalAuto)
	oldCtrl.SetPlanMode(true)
	m := newChatTUI(oldCtrl, "", make(chan event.Event, 1), 100)
	m.modelRef = "provider/model"
	m.runtimeProfile = boot.TokenModeFull
	builds := 0
	m.buildController = func(controllerBuildSpec, []provider.Message, string, control.SessionAPI) (*control.Controller, error) {
		builds++
		return control.New(control.Options{Label: "new"}), nil
	}

	cmd := m.runWorkModeCommand("/preset delivery")
	if cmd != nil {
		t.Fatal("in-place preset switch must not schedule a controller rebuild")
	}
	if m.ctrl != oldCtrl {
		t.Fatal("controller must stay the same instance")
	}
	if m.runtimeProfile != boot.TokenModeDelivery {
		t.Fatalf("runtime profile = %q, want delivery", m.runtimeProfile)
	}
	if m.ctrl.AgentPreset() != boot.AgentPresetDelivery {
		t.Fatalf("controller preset = %q, want delivery", m.ctrl.AgentPreset())
	}
	if builds != 0 {
		t.Fatalf("unexpected rebuilds: %d", builds)
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
		"/preset unknown",
		"/preset balanced",
		"/preset economy extra",
	} {
		if cmd := m.runWorkModeCommand(input); cmd != nil {
			t.Fatalf("%q unexpectedly scheduled a rebuild", input)
		}
	}
	m.pendingApproval = &event.Approval{ID: "approval", Tool: "bash"}
	if cmd := m.runWorkModeCommand("/preset light"); cmd != nil {
		t.Fatal("pending approval should block preset switching")
	}
	if m.runtimeProfile != boot.TokenModeFull {
		t.Fatalf("busy switch changed profile to %q", m.runtimeProfile)
	}
	if builds != 0 {
		t.Fatalf("rejected preset requests triggered %d builds", builds)
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
	if cmd := m.runWorkModeCommand("/preset delivery"); cmd != nil {
		t.Fatal("running turn should block preset switching")
	}
	if m.runtimeProfile != boot.TokenModeFull {
		t.Fatalf("running-turn switch changed profile to %q", m.runtimeProfile)
	}
	if builds != 0 {
		t.Fatalf("running-turn guard triggered %d builds", builds)
	}
}
