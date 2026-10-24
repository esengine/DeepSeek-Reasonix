package boot

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/runtime/isolation"
	"reasonix/internal/session/control"
	"reasonix/internal/state/mutation"
)

type nestedCancelProvider struct {
	nestedContinuationProvider
	entered chan struct{}
	once    sync.Once
}

func (p *nestedCancelProvider) Stream(ctx context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	if _, written := callResult(req, "create-file"); written && strings.Contains(firstUser(req), "NEST-CONT-LEAF") {
		p.once.Do(func() { close(p.entered) })
		<-ctx.Done()
		return nil, ctx.Err()
	}
	return p.nestedContinuationProvider.Stream(ctx, req)
}

func TestEffectNestedCancellationRetainsWorkAndLeavesSourceTransactionsAvailable(t *testing.T) {
	for _, background := range []bool{false, true} {
		t.Run(map[bool]string{false: "foreground", true: "background"}[background], func(t *testing.T) {
			p := &nestedCancelProvider{nestedContinuationProvider: nestedContinuationProvider{background: background}, entered: make(chan struct{})}
			opts := nestedExecutionOptions(t, p, "", "", event.Discard)
			b := newBuilder()
			built, err := b.build(t.Context(), opts)
			if err != nil {
				t.Fatal(err)
			}
			ctrl := built.Controller
			bindPresenceSession(t, ctrl)
			t.Cleanup(ctrl.Close)
			ctrl.SetToolApprovalMode(control.ToolApprovalYolo)
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			defer cancel()
			done := make(chan error, 1)
			go func() { done <- ctrl.Run(ctx, "cancel a nested writer after its first mutation") }()
			select {
			case <-p.entered:
			case err := <-done:
				t.Fatalf("nested execution returned before its model wait: %v", err)
			case <-ctx.Done():
				t.Fatal(ctx.Err())
			}
			path := filepath.Join(opts.WorkspaceRoot, "unrelated.txt")
			lease, err := b.session.mutations.Acquire(ctx, mutation.Intent{Effect: mutation.NamedPaths, Paths: []string{path}, CallID: "fixture.parallel_source_write"})
			if err != nil {
				ctrl.Cancel()
				<-done
				t.Fatalf("nested model inference retained source resources: %v", err)
			}
			err = os.WriteFile(path, []byte("parallel source change\n"), 0644)
			lease.End(mutation.Committed)
			if err != nil {
				ctrl.Cancel()
				<-done
				t.Fatal(err)
			}
			ctrl.Cancel()
			if err := <-done; !errors.Is(err, context.Canceled) {
				t.Fatalf("nested execution lost cancellation: %v", err)
			}
			catalog, err := ctrl.IsolationResults(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			var child *isolation.ResultSummary
			for index := range catalog.Results {
				row := &catalog.Results[index]
				if row.Kind != "result" {
					continue
				}
				if row.Activity != "inactive" || row.State == isolation.Running || row.CleanupComplete {
					t.Fatalf("cancelled dependency still runs or lost retained work: %+v", row)
				}
				if row.ParentID != "" {
					child = row
				}
			}
			if child == nil || child.State != isolation.Cancelled {
				t.Fatalf("cancelled descendant lost its lifecycle: %+v", catalog)
			}
			if raw, err := os.ReadFile(filepath.Join(child.CandidateRoot, "leaf.txt")); err != nil || string(raw) != "first\n" {
				t.Fatalf("cancellation discarded actual candidate writes: %q %v", raw, err)
			}
			if _, err := os.Stat(filepath.Join(opts.WorkspaceRoot, "leaf.txt")); !os.IsNotExist(err) {
				t.Fatalf("cancelled nested execution published into source: %v", err)
			}
		})
	}
}
