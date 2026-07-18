package host

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"reasonix/internal/billing"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/jobs"
	"reasonix/internal/provider"
	"reasonix/internal/remote/idempotency"
	"reasonix/internal/remote/protocol"
)

type statusQueryController struct {
	*fakeSessionController
	root string

	statusMu   sync.Mutex
	jobValues  []jobs.View
	lastUsage  provider.Usage
	balance    *billing.Balance
	balanceErr error
	cancelled  []string
}

func (c *statusQueryController) WorkspaceRoot() string       { return c.root }
func (c *statusQueryController) ContextSnapshot() (int, int) { return 12, 128 }
func (c *statusQueryController) LastUsage() *provider.Usage {
	c.statusMu.Lock()
	defer c.statusMu.Unlock()
	value := c.lastUsage
	return &value
}
func (c *statusQueryController) Jobs() []jobs.View {
	c.statusMu.Lock()
	defer c.statusMu.Unlock()
	return append([]jobs.View(nil), c.jobValues...)
}
func (c *statusQueryController) Balance(context.Context) (*billing.Balance, error) {
	c.statusMu.Lock()
	defer c.statusMu.Unlock()
	return c.balance, c.balanceErr
}
func (c *statusQueryController) CancelBackgroundJob(jobID string) bool {
	c.statusMu.Lock()
	defer c.statusMu.Unlock()
	for index, item := range c.jobValues {
		if item.ID != jobID || item.Status != "running" {
			continue
		}
		c.jobValues = append(c.jobValues[:index], c.jobValues[index+1:]...)
		c.cancelled = append(c.cancelled, jobID)
		return true
	}
	return false
}

type statusQueryFactory struct {
	root       string
	controller *statusQueryController
}

func (f *statusQueryFactory) CreateController(ctx context.Context, _ protocol.RuntimeTarget, sink event.Sink) (control.SessionAPI, error) {
	base := control.New(control.Options{Sink: sink, WorkspaceRoot: f.root})
	fake := newFakeSessionController(ctx, sink)
	fake.SessionAPI = base
	f.controller = &statusQueryController{
		fakeSessionController: fake, root: f.root,
		lastUsage: provider.Usage{PromptTokens: 7, CompletionTokens: 5, TotalTokens: 12},
		balance:   &billing.Balance{Available: true, Infos: []billing.Info{{Currency: "USD", TotalBalance: "4.20"}}},
	}
	for index := 0; index < 205; index++ {
		f.controller.jobValues = append(f.controller.jobValues, jobs.View{
			ID: fmt.Sprintf("job-%03d", index), Kind: "bash", Label: fmt.Sprintf("job %d", index),
			Status: "running", StartedAt: int64(index),
		})
	}
	return f.controller, nil
}

func newStatusQueryRuntime(t *testing.T) (*RuntimeManager, *SessionRuntime, *statusQueryController) {
	t.Helper()
	t.Setenv("REASONIX_HOME", filepath.Join(t.TempDir(), "reasonix-home"))
	root := t.TempDir()
	factory := &statusQueryFactory{root: root}
	manager, err := NewRuntimeManager(context.Background(), "host-status", factory, RuntimeManagerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(manager.Close)
	target := protocol.RuntimeTarget{WorkspaceID: "workspace-status", SessionID: "session-status"}
	runtime, err := manager.GetOrCreate(target)
	if err != nil {
		t.Fatal(err)
	}
	return manager, runtime, factory.controller
}

func statusRuntimeQuery(runtime *SessionRuntime) protocol.RuntimeQuery {
	return protocol.RuntimeQuery{
		ExpectedHostEpoch: "host-status", Target: runtime.Target(), ExpectedRuntimeEpoch: runtime.Epoch(),
	}
}

func TestStatusCatalogQueriesUseActorBarrierAndSharedProjection(t *testing.T) {
	_, runtime, controller := newStatusQueryRuntime(t)
	query := statusRuntimeQuery(runtime)
	var guards atomic.Int64
	guard := func() error { guards.Add(1); return nil }

	catalog, err := runtime.SessionCatalogQuery(context.Background(), query, guard)
	if err != nil || catalog.Revision == "" || len(catalog.Commands) == 0 {
		t.Fatalf("catalog = %+v, %v", catalog, err)
	}
	again, err := runtime.SessionCatalogQuery(context.Background(), query, guard)
	if err != nil || again.Revision != catalog.Revision {
		t.Fatalf("catalog revision changed: %q -> %q, %v", catalog.Revision, again.Revision, err)
	}
	contextView, err := runtime.SessionContextQuery(context.Background(), query, guard)
	if err != nil || contextView.UsedTokens != 12 || contextView.WindowTokens != 128 || contextView.PromptTokens != 7 {
		t.Fatalf("context = %+v, %v", contextView, err)
	}
	slash, err := runtime.ComposerSlashArgsQuery(context.Background(), protocol.ComposerSlashArgsParams{
		RuntimeQuery: query, Input: "/goal ",
	}, guard)
	if err != nil || len(slash.Items) != 4 || slash.From != len("/goal ") {
		t.Fatalf("slash args = %+v, %v", slash, err)
	}
	balance, err := runtime.SessionBalanceQuery(context.Background(), query, guard)
	if err != nil || !balance.Available || balance.Display != "$4.20" {
		t.Fatalf("balance = %+v, %v", balance, err)
	}
	if guards.Load() != 6 { // catalog twice, context, slash, and balance pre/post
		t.Fatalf("query guard calls = %d, want 6", guards.Load())
	}

	controller.statusMu.Lock()
	controller.balanceErr = errors.New("provider https://secret.invalid token=secret")
	controller.statusMu.Unlock()
	balance, err = runtime.SessionBalanceQuery(context.Background(), query, nil)
	if err != nil || balance.Available || balance.Display != "" {
		t.Fatalf("provider failure balance = %+v, %v", balance, err)
	}

	stale := query
	stale.ExpectedRuntimeEpoch = "runtime-stale"
	if _, err := runtime.SessionContextQuery(context.Background(), stale, nil); err == nil {
		t.Fatal("stale runtime epoch accepted")
	}
}

func TestJobListPagingCancelIdempotencyAndNotRunning(t *testing.T) {
	_, runtime, controller := newStatusQueryRuntime(t)
	query := statusRuntimeQuery(runtime)
	first, err := runtime.ListJobsQuery(context.Background(), protocol.JobListParams{RuntimeQuery: query}, nil)
	if err != nil || len(first.Jobs) != 200 || !first.HasMore || first.Next == "" {
		t.Fatalf("first jobs = len %d, more %v, next %q, %v", len(first.Jobs), first.HasMore, first.Next, err)
	}
	second, err := runtime.ListJobsQuery(context.Background(), protocol.JobListParams{
		RuntimeQuery: query, Cursor: protocol.Cursor(first.Next),
	}, nil)
	if err != nil || len(second.Jobs) != 5 || second.HasMore {
		t.Fatalf("second jobs = %+v, %v", second, err)
	}

	registry, err := idempotency.New("host-status", idempotency.Options{})
	if err != nil {
		t.Fatal(err)
	}
	params := protocol.JobCancelParams{
		SessionMutation: protocol.SessionMutation{
			RequestID: "cancel-job-request", ExpectedHostEpoch: "host-status",
			Target: runtime.Target(), ExpectedRuntimeEpoch: runtime.Epoch(),
		},
		JobID: "job-000",
	}
	cancelled, err := runtime.CancelJobMutation(context.Background(), registry, params, nil)
	if err != nil || cancelled.Disposition != protocol.JobCancelled {
		t.Fatalf("cancelled = %+v, %v", cancelled, err)
	}
	replay, err := runtime.CancelJobMutation(context.Background(), registry, params, nil)
	if err != nil || replay != cancelled {
		t.Fatalf("cancel replay = %+v, %v", replay, err)
	}
	params.RequestID = "cancel-missing-request"
	params.JobID = "job-missing"
	missing, err := runtime.CancelJobMutation(context.Background(), registry, params, nil)
	if err != nil || missing.Disposition != protocol.JobNotRunning {
		t.Fatalf("missing cancel = %+v, %v", missing, err)
	}
	controller.statusMu.Lock()
	defer controller.statusMu.Unlock()
	if len(controller.cancelled) != 1 || controller.cancelled[0] != "job-000" {
		t.Fatalf("controller cancellations = %v", controller.cancelled)
	}
}

func TestJobCancelWithoutFocusedControllerPortIsCapabilityUnavailable(t *testing.T) {
	manager, runtime, _ := newStatusQueryRuntime(t)
	_ = manager
	// Remove the optional method by installing the embedded legacy fake in a
	// separate runtime manager.
	factory := ControllerFactoryFunc(func(ctx context.Context, _ protocol.RuntimeTarget, sink event.Sink) (control.SessionAPI, error) {
		fake := newFakeSessionController(ctx, sink)
		fake.SessionAPI = control.New(control.Options{Sink: sink})
		return fake, nil
	})
	legacyManager, err := NewRuntimeManager(context.Background(), "host-legacy", factory, RuntimeManagerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(legacyManager.Close)
	legacy, err := legacyManager.GetOrCreate(protocol.RuntimeTarget{WorkspaceID: "workspace-legacy", SessionID: "session-legacy"})
	if err != nil {
		t.Fatal(err)
	}
	registry, _ := idempotency.New("host-legacy", idempotency.Options{})
	_, err = legacy.CancelJobMutation(context.Background(), registry, protocol.JobCancelParams{
		SessionMutation: protocol.SessionMutation{
			RequestID: "legacy-job-cancel", ExpectedHostEpoch: "host-legacy",
			Target: legacy.Target(), ExpectedRuntimeEpoch: legacy.Epoch(),
		},
		JobID: "missing",
	}, nil)
	var remoteErr *protocol.RemoteError
	if !errors.As(err, &remoteErr) || remoteErr.Code != protocol.ErrCapabilityUnavailable {
		t.Fatalf("legacy job cancel error = %v", err)
	}
	_ = runtime
}
