package boot

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/control"
)

// Deterministic coverage for gateExtensionUIRequest: a sidecar's startup
// host/ui/request must WAIT for the controller instead of failing outright
// (the pre-preflight lifecycle started sidecars only after control.New, so
// these requests always found a controller).

func TestGateExtensionUIRequestWaitsForControllerReady(t *testing.T) {
	var ctrlRef atomic.Pointer[control.Controller]
	ready := make(chan struct{})
	failed := make(chan struct{})

	type result struct {
		values map[string]any
		err    error
	}
	resCh := make(chan result, 1)
	go func() {
		values, _, err := gateExtensionUIRequest(context.Background(), ctrlRef.Load, ready, failed,
			func(c *control.Controller) (map[string]any, bool, error) {
				return map[string]any{"answer": "yes"}, false, nil
			})
		resCh <- result{values, err}
	}()

	// The request is parked: nothing may come back before readiness.
	select {
	case r := <-resCh:
		t.Fatalf("request resolved before the controller was ready: %+v", r)
	case <-time.After(50 * time.Millisecond):
	}

	ctrlRef.Store(&control.Controller{})
	close(ready)
	select {
	case r := <-resCh:
		if r.err != nil || r.values["answer"] != "yes" {
			t.Fatalf("request after ready: values=%v err=%v", r.values, r.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("request did not resolve after the controller became ready")
	}
}

func TestGateExtensionUIRequestUnblocksOnBuildFailure(t *testing.T) {
	var ctrlRef atomic.Pointer[control.Controller]
	ready := make(chan struct{})
	failed := make(chan struct{})

	errCh := make(chan error, 1)
	go func() {
		_, _, err := gateExtensionUIRequest(context.Background(), ctrlRef.Load, ready, failed,
			func(c *control.Controller) (map[string]any, bool, error) {
				return nil, false, errors.New("serve must not run on a failed build")
			})
		errCh <- err
	}()

	close(failed)
	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("expected a build-failure error, got nil")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("request stayed parked after the build failed")
	}
}

func TestGateExtensionUIRequestHonorsRequestCancellation(t *testing.T) {
	var ctrlRef atomic.Pointer[control.Controller]
	ready := make(chan struct{})
	failed := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())

	errCh := make(chan error, 1)
	go func() {
		_, _, err := gateExtensionUIRequest(ctx, ctrlRef.Load, ready, failed,
			func(c *control.Controller) (map[string]any, bool, error) {
				return nil, false, errors.New("serve must not run on a cancelled request")
			})
		errCh <- err
	}()

	cancel()
	select {
	case err := <-errCh:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context.Canceled, got %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("request stayed parked after its context cancelled")
	}
}
