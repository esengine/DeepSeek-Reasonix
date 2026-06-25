package sandbox

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestExecutionEnforcesStepAndToolLimits(t *testing.T) {
	now := time.Now().UTC()
	exec := Start(SandboxContext{MaxSteps: 1, MaxTimeMs: 1000, ToolCallLimit: 1}, now)
	if err := exec.Step(now); err != nil {
		t.Fatal(err)
	}
	if err := exec.Step(now); err == nil || !strings.Contains(err.Error(), "max steps exceeded") {
		t.Fatalf("second step error = %v, want max steps exceeded", err)
	}

	exec = Start(SandboxContext{MaxSteps: 3, MaxTimeMs: 1000, ToolCallLimit: 1}, now)
	if err := exec.AddToolCalls(2, now); err == nil || !strings.Contains(err.Error(), "tool call limit exceeded") {
		t.Fatalf("tool call error = %v, want tool call limit exceeded", err)
	}
}

func TestExecutionKillSwitchTerminatesContext(t *testing.T) {
	now := time.Now().UTC()
	exec := Start(DefaultContext(), now)
	exec.Kill("operator stop", now)
	if err := exec.Step(now); err == nil || !strings.Contains(err.Error(), "operator stop") {
		t.Fatalf("step after kill error = %v, want operator stop", err)
	}
	snap := exec.Snapshot()
	if snap.KillReason != "operator stop" || snap.TerminatedAt.IsZero() {
		t.Fatalf("invalid kill snapshot: %+v", snap)
	}
}

func TestRunIsolatedClonesContextAndDetectsLeaks(t *testing.T) {
	type ctxKey string
	parent := context.WithValue(context.Background(), ctxKey("secret"), "shared")
	now := time.Now().UTC()
	snap, err := RunIsolated(parent, SandboxContext{MaxTimeMs: 1000}, now, func(ctx context.Context) error {
		if got := ctx.Value(ctxKey("secret")); got != nil {
			t.Fatalf("isolated context leaked parent value: %v", got)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if !snap.Isolation.Completed || !snap.Isolation.Policy.StrictContextClone || !snap.Isolation.Policy.GoroutineContainment {
		t.Fatalf("invalid isolation snapshot: %+v", snap.Isolation)
	}

	snap, err = RunIsolated(context.Background(), SandboxContext{MaxTimeMs: 1}, now, func(ctx context.Context) error {
		<-make(chan struct{})
		return nil
	})
	if err == nil {
		t.Fatal("expected isolated run to time out")
	}
	if !snap.Isolation.TimedOut || !snap.Isolation.PotentialLeak {
		t.Fatalf("leaking goroutine was not detected: %+v", snap.Isolation)
	}
	if !HasActiveEscape(snap.Isolation.EscapeReport) {
		t.Fatalf("leaking goroutine did not create active escape report: %+v", snap.Isolation.EscapeReport)
	}
	got := snap.Isolation.EscapeReport.Active[0]
	if got.Class != "goroutine_leak" || got.Severity != "high" {
		t.Fatalf("unexpected escape finding: %+v", got)
	}
	if len(snap.Isolation.EscapeReport.ResidualRisks) == 0 {
		t.Fatalf("missing residual process-boundary risk: %+v", snap.Isolation.EscapeReport)
	}
}

func TestClassifyEscapeRisksSeparatesResidualProcessRisk(t *testing.T) {
	report := ClassifyEscapeRisks(IsolationSnapshot{Policy: DefaultIsolationPolicy(), Completed: true})
	if HasActiveEscape(report) {
		t.Fatalf("residual process boundary risk should not be active: %+v", report)
	}
	if len(report.ResidualRisks) != 1 || report.ResidualRisks[0].Class != "process_boundary_absent" {
		t.Fatalf("missing residual process boundary risk: %+v", report)
	}
}
