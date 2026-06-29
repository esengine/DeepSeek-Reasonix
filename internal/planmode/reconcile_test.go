package planmode_test

import (
	"testing"

	"reasonix/internal/planmode"
	"reasonix/internal/tool"

	// Blank import so the built-in tools self-register via their init()s and
	// tool.Builtins() returns the real compile-time roster.
	_ "reasonix/internal/tool/builtin"
)

// TestEveryBuiltinHasExplicitPlanModeStance is the forcing function against
// plan-mode drift. It walks the real built-in roster and asserts every tool
// lands in an explicit plan-mode bucket. A newly added built-in that nobody
// classified falls through to ClassDefaultBlocked and turns this test red,
// forcing the author to record a stance — in planSafeReadOnly, knownBlockedTools,
// alwaysAllowedTools, or via tool.PlanModeClassifier. It also enforces the
// PlanSafe ⇒ ReadOnly invariant for the plan-safe buckets.
//
// This covers built-ins only (tool.Builtins()). Non-built-in tools added at boot
// (read_only_task, MCP/plugin tools, economy sources) are covered by the marker
// round-trip and the agent/boot integration tests instead.
func TestEveryBuiltinHasExplicitPlanModeStance(t *testing.T) {
	builtins := tool.Builtins()
	if len(builtins) == 0 {
		t.Fatal("tool.Builtins() is empty — built-in registration did not run")
	}
	for _, tl := range builtins {
		name := tl.Name()
		safety := planmode.PlanSafetyUnknown
		if c, ok := tl.(tool.PlanModeClassifier); ok {
			if c.PlanModeSafe() {
				safety = planmode.PlanSafetySafe
			} else {
				safety = planmode.PlanSafetyUnsafe
			}
		}
		class := planmode.Classify(name, tl.ReadOnly(), safety)
		if class == planmode.ClassDefaultBlocked {
			t.Errorf("built-in %q has no explicit plan-mode stance (ReadOnly=%v, safety=%v).\n"+
				"Record one: add it to planSafeReadOnly or knownBlockedTools, or implement "+
				"tool.PlanModeClassifier on the tool.", name, tl.ReadOnly(), safety)
		}
		switch class {
		case planmode.ClassPlanSafeSelfReported, planmode.ClassPlanSafeAudited:
			if !tl.ReadOnly() {
				t.Errorf("built-in %q is classified plan-safe but ReadOnly()==false — "+
					"violates the PlanSafe ⇒ ReadOnly invariant", name)
			}
		}
	}
}

// TestClassifyFlagsUnclassifiedTool guards the forcing function itself: an
// unregistered tool name reporting plain ReadOnly() with no self-report must
// classify as ClassDefaultBlocked, which is exactly the signal the roster test
// above keys off. If this ever stops holding, the roster test goes blind.
func TestClassifyFlagsUnclassifiedTool(t *testing.T) {
	if c := planmode.Classify("brand_new_unregistered_tool", true, planmode.PlanSafetyUnknown); c != planmode.ClassDefaultBlocked {
		t.Fatalf("an unclassified tool must be ClassDefaultBlocked, got %v", c)
	}
}
