package boot

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/runtime/delegation"
	"reasonix/internal/runtime/isolation"
	"reasonix/internal/runtime/writeclaim"
	"reasonix/internal/session/control"
	"reasonix/internal/state/mutation"
	"reasonix/internal/state/sessionstore"
)

func integrationBootFixture(t *testing.T, sink event.Sink) (*builder, Options, *isolation.Entry, delegation.ProfileExecSpec) {
	t.Helper()
	home, root := statedBootHome(t), t.TempDir()
	kind := "integration-lifecycle-" + strings.ToLower(t.Name())
	provider.Register(kind, func(provider.Config) (provider.Provider, error) { return &hostCheckScriptProvider{}, nil })
	writeFile(t, root, "reasonix.toml", `default_model="fixture"
[agent]
collaboration="shared"
max_subagent_concurrency=1
[codegraph]
enabled=false
[[providers]]
name="fixture"
kind="`+kind+`"
model="fixture"
`)
	writeFile(t, root, "REASONIX.md", "## Reasonix host checks\n\n- verify: git diff --check\n")
	mirrorToUserConfigAt(t, home, root)
	gitInDir(t, root, "init", "-q")
	gitInDir(t, root, "add", "-A")
	gitInDir(t, root, "commit", "-qm", "fixture")
	b := newBuilder()
	built, err := b.build(t.Context(), Options{Home: home, WorkspaceRoot: root, Sink: sink})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(built.Controller.Close)
	built.Controller.EnsureSessionPath()
	built.Controller.SetToolApprovalMode(control.ToolApprovalYolo)
	entry, err := b.tools.isolation.store.BeginOwned(t.Context(), root, sessionstore.BranchID(built.Controller.SessionPath()))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(entry.Cand.WorkspaceRoot, "out.txt"), []byte("candidate\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if _, err := b.tools.isolation.store.Complete(t.Context(), entry, isolation.Outcome{}, nil); err != nil {
		t.Fatal(err)
	}
	parent := b.opts
	parent.frozenConfig, err = b.cfg.CloneResolved()
	if err != nil {
		t.Fatal(err)
	}
	parent.DelegationScheduler = b.tools.taskTool.Scheduler()
	grant, err := writeclaim.WholeWorkspaceWriteClaim(root)
	if err != nil {
		t.Fatal(err)
	}
	spec := delegation.ProfileExecSpec{Worker: delegation.WorkerSpec{Kind: "task", Name: "integration", Model: b.model.name},
		Grant: delegation.CapabilityGrant{CallTools: []string{"bash"}, WritePaths: grant}, Sched: delegation.SchedulerPolicy{MaxSteps: 4}}
	return b, parent, entry, spec
}

func TestEffectIntegrationSnapshotWaitsForRootPreparationCapacity(t *testing.T) {
	waiting := make(chan struct{}, 1)
	b, parent, entry, spec := integrationBootFixture(t, event.FuncSink(func(e event.Event) {
		if w := e.WorkspaceLease; w != nil && w.CallID == "isolation.integration.snapshot" && w.State == "waiting" {
			select {
			case waiting <- struct{}{}:
			default:
			}
		}
	}))
	capacity := parent.DelegationScheduler.PhaseCapacity().Preparation
	if len(capacity) != 1 {
		t.Fatal("fixture lacks bounded root preparation pool")
	}
	held, err := b.session.mutations.Acquire(t.Context(), mutation.Intent{Effect: mutation.CapacityOnly, Capacities: capacity})
	if err != nil {
		t.Fatal(err)
	}
	defer held.End(mutation.NotExecuted)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := verifyAndIntegrate(ctx, parent, b.tools.isolation.store, b.tools.isolation.posture, b.root, spec, entry, event.Discard,
			func(context.Context, []string) error { return nil }, b.prompt.projectChecks)
		done <- err
	}()
	select {
	case <-waiting:
	case <-time.After(5 * time.Second):
		cancel()
		<-done
		t.Fatal("integration source snapshot bypassed root preparation admission")
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("queued integration swallowed cancellation: %v", err)
	}
	if attempts := b.tools.isolation.store.IntegrationAttempts(); len(attempts) != 0 {
		t.Fatalf("unadmitted integration created retained candidates: %+v", attempts)
	}
	held.End(mutation.NotExecuted)
	probeCtx, stop := context.WithTimeout(t.Context(), time.Second)
	defer stop()
	probe, err := b.session.mutations.Acquire(probeCtx, mutation.Intent{Effect: mutation.CapacityOnly, Capacities: capacity})
	if err != nil {
		t.Fatalf("cancelled integration retained preparation capacity: %v", err)
	}
	probe.End(mutation.NotExecuted)
}

type integrationPanicResolver struct{ provider.Resolver }

func (integrationPanicResolver) Resolve(provider.Selection) (provider.Provider, error) {
	panic("fixture integration kernel failure")
}

func TestEffectIntegrationBuildPanicRetainsResultsAndReleasesPreparation(t *testing.T) {
	b, parent, entry, spec := integrationBootFixture(t, event.Discard)
	parent.ProviderResolver = integrationPanicResolver{b.providers.effective}
	_, err := verifyAndIntegrate(t.Context(), parent, b.tools.isolation.store, b.tools.isolation.posture, b.root, spec, entry, event.Discard,
		func(context.Context, []string) error { return nil }, b.prompt.projectChecks)
	if err == nil || !strings.Contains(err.Error(), "panicked") {
		t.Fatalf("integration build panic escaped its result boundary: %v", err)
	}
	if _, err := os.Stat(filepath.Join(b.root, "out.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed integration wrote the source: %v", err)
	}
	if attempts := b.tools.isolation.store.IntegrationAttempts(); len(attempts) != 1 || attempts[0].Active {
		t.Fatalf("failed build lost retained attempt or ownership release: %+v", attempts)
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	lease, err := b.session.mutations.Acquire(ctx, mutation.Intent{Effect: mutation.CapacityOnly, Capacities: parent.DelegationScheduler.PhaseCapacity().Preparation})
	if err != nil {
		t.Fatalf("panic retained preparation capacity: %v", err)
	}
	lease.End(mutation.NotExecuted)
}
