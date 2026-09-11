package boot

// Effect tests for the session quality floor: the floor must reach the
// controller without touching the provider-visible prefix.

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"reasonix/internal/ablation"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/provider"
)

// TestEffectQualityFloorDoesNotTouchProviderPrefix pins the cache contract:
// standard and delivery send identical system prompts and tool schemas. The
// workspace line embeds the run's temp dir, so both sides normalize it.
func TestEffectQualityFloorDoesNotTouchProviderPrefix(t *testing.T) {
	standard := effectRun(t, "boot-effect-floor-standard", "", ablation.Set{})
	delivery := effectRun(t, "boot-effect-floor-delivery", "delivery", ablation.Set{})

	stdReq, delReq := standard[0], delivery[0]
	stdPrompt := stripWorkspaceLine(systemMessage(stdReq.Messages))
	delPrompt := stripWorkspaceLine(systemMessage(delReq.Messages))
	if stdPrompt != delPrompt {
		t.Fatalf("delivery floor changed the provider-visible system prompt\n%s", firstPromptDiff(stdPrompt, delPrompt))
	}
	if !reflect.DeepEqual(toolSchemaNames(stdReq.Tools), toolSchemaNames(delReq.Tools)) {
		t.Fatalf("delivery floor changed tool schemas\nstandard=%v\ndelivery=%v",
			toolSchemaNames(stdReq.Tools), toolSchemaNames(delReq.Tools))
	}
}

// firstPromptDiff names the first differing line so a failure is diagnosable
// instead of reporting only that the two prefixes are unequal.
func firstPromptDiff(want, got string) string {
	a, b := strings.Split(want, "\n"), strings.Split(got, "\n")
	for i := 0; i < len(a) || i < len(b); i++ {
		var x, y string
		if i < len(a) {
			x = a[i]
		}
		if i < len(b) {
			y = b[i]
		}
		if x != y {
			return fmt.Sprintf("line %d:\n  standard: %q\n  delivery: %q", i+1, x, y)
		}
	}
	return "no line differs (length mismatch only)"
}

func stripWorkspaceLine(s string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		if strings.HasPrefix(l, "Current workspace:") {
			lines[i] = "Current workspace: <ROOT>"
		}
	}
	return strings.Join(lines, "\n")
}

// TestEffectDeliveryFloorSetsSessionFloor asserts the role input reaches the
// controller's floor, and light folds to standard.
func TestEffectDeliveryFloorSetsSessionFloor(t *testing.T) {
	if got := effectFloorController(t, "delivery").QualityFloor(); got != "delivery" {
		t.Fatalf("QualityFloor = %q, want delivery", got)
	}
	if got := effectFloorController(t, "light").QualityFloor(); got != "standard" {
		t.Fatalf("light must fold to standard, got %q", got)
	}
	if got := effectFloorController(t, "").QualityFloor(); got != "standard" {
		t.Fatalf("default floor = %q, want standard", got)
	}
}

// effectFloorController builds the real stack with a role input and returns
// the controller; no turn runs.
func effectFloorController(t *testing.T, tokenMode string) *control.Controller {
	t.Helper()
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Chdir(dir)
	kind := "boot-effect-floor-ctl"
	if tokenMode != "" {
		kind += "-" + tokenMode
	}
	provider.Register(kind, func(provider.Config) (provider.Provider, error) {
		return &effectRecordingProvider{}, nil
	})
	writeFile(t, dir, "reasonix.toml", `
default_model = "floor-model"

[[providers]]
name = "floor-model"
kind = "`+kind+`"
model = "x"
`)
	ctrl, err := Build(context.Background(), Options{Sink: event.Discard, TokenMode: tokenMode})
	if err != nil {
		t.Fatalf("Build(%q): %v", tokenMode, err)
	}
	t.Cleanup(func() { ctrl.Close() })
	return ctrl
}
