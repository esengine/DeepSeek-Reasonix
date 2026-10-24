package mutation

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"reasonix/internal/state/workspacelease"
)

func TestLegacyWaitClosesWithoutPrematureAcquisition(t *testing.T) {
	root, state := t.TempDir(), t.TempDir()
	legacy, err := workspacelease.New(root, state, nil)
	if err != nil {
		t.Fatal(err)
	}
	legacy.BeginRun()
	defer legacy.EndRun()
	path := filepath.Join(root, "a")
	if err := legacy.AcquirePaths(t.Context(), []string{path}); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var states []string
	c, err := New(root, state, func(n Notice) {
		states = append(states, n.State)
		if n.State == "waiting" {
			cancel()
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Acquire(ctx, intent(path)); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if !slices.Equal(states, []string{"waiting", "abandoned"}) {
		t.Fatalf("events: %v", states)
	}
	legacy.EndRun()
	lease, err := c.Acquire(t.Context(), intent(path))
	if err != nil {
		t.Fatalf("refusal retained a resource: %v", err)
	}
	lease.End(NotExecuted)
}

func TestWaitNoticesRetainIdentityAndTerminationReason(t *testing.T) {
	for _, finish := range []string{"acquired", "cancelled", "conflict"} {
		t.Run(finish, func(t *testing.T) {
			root, state := t.TempDir(), t.TempDir()
			path := filepath.Join(root, "a")
			holder := testCoordinator(t, root, state)
			held, err := holder.Acquire(t.Context(), intent(path))
			if err != nil {
				t.Fatal(err)
			}
			defer held.End(NotExecuted)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			var notices []Notice
			c, err := New(root, state, func(n Notice) {
				if n.State == "released" {
					return
				}
				notices = append(notices, n)
				if n.State != "waiting" {
					return
				}
				switch finish {
				case "cancelled":
					cancel()
				case "conflict":
					if err := os.WriteFile(path, []byte("changed"), 0600); err != nil {
						t.Fatal(err)
					}
					held.End(Committed)
				default:
					held.End(NotExecuted)
				}
			})
			if err != nil {
				t.Fatal(err)
			}
			request := intent(path)
			request.Expected = map[string]ContentVersion{path: {Kind: "absent"}}
			lease, err := c.Acquire(ctx, request)
			if lease != nil {
				lease.End(NotExecuted)
			}
			if (finish == "acquired") != (err == nil) {
				t.Fatalf("acquire: %v", err)
			}
			if len(notices) != 2 {
				t.Fatalf("notices: %+v", notices)
			}
			start, end := notices[0], notices[1]
			if start.TransactionID == "" || end.TransactionID != start.TransactionID || end.StartedAt != start.StartedAt || start.State != "waiting" {
				t.Fatalf("unpaired wait: %+v", notices)
			}
			if start.HolderTransactionID != held.ID || start.HolderExecutionID != "execution" || len(start.HolderPaths) != 1 {
				t.Fatalf("missing holder: %+v", start)
			}
			if finish == "acquired" {
				if end.State != "acquired" || end.Reason != "" {
					t.Fatalf("end: %+v", end)
				}
			} else if end.State != "abandoned" || end.Reason != finish {
				t.Fatalf("end: %+v", end)
			}
		})
	}
}
