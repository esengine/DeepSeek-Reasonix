package isolation

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"reasonix/internal/contract/tool"
	"reasonix/internal/state/mutation"
)

func TestRevocationFencesManualApplyAlreadyWaitingForResources(t *testing.T) {
	s, request, e := importedFixture(t)
	waiting := make(chan struct{}, 1)
	coordinator, err := mutation.New(s.workspace, t.TempDir(), func(n mutation.Notice) {
		if n.CallID == "isolation.apply" && n.State == "waiting" {
			waiting <- struct{}{}
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	s.WithMutationCoordinator(coordinator)
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	l, err := coordinator.Acquire(ctx, mutation.Intent{Effect: mutation.NamedPaths, Paths: []string{s.workspace}})
	if err != nil {
		t.Fatal(err)
	}
	defer l.End(mutation.NotExecuted)
	done := make(chan error, 1)
	go func() { _, err := s.Apply(ctx, e.ID); done <- err }()
	select {
	case <-waiting:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	if err := s.RevokeImportedResult(ctx, request, func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	l.End(mutation.NotExecuted)
	var refusal tool.Refusal
	if err := <-done; !errors.As(err, &refusal) || refusal.Code != CodeStaleExecution {
		t.Fatalf("revoked apply reached source: %v", err)
	}
	if got := read(t, s.workspace, "a.txt"); got != "one\n" {
		t.Fatal(got)
	}
}

func TestRevocationFencesVerifiedCommitAfterFinalAuthorization(t *testing.T) {
	s, request, e := importedFixture(t)
	if err := os.Chmod(s.workspace, 0755); err != nil {
		t.Fatal(err)
	}
	i, err := s.PrepareIntegration(t.Context(), e.ID, request.Owner)
	if err != nil {
		t.Fatal(err)
	}
	defer i.Close()
	checkPreparedIntegration(t, s, i)
	calls := 0
	err = i.Commit(t.Context(), func(ctx context.Context, _ []string) error {
		calls++
		if calls == 2 {
			return s.RevokeImportedResult(ctx, request, func() error { return nil })
		}
		return nil
	})
	var refusal tool.Refusal
	if !errors.As(err, &refusal) || refusal.Code != CodeStaleExecution || calls != 2 {
		t.Fatalf("commit ignored revocation after authorization: %v calls=%d", err, calls)
	}
	if got := read(t, s.workspace, "a.txt"); got != "one\n" {
		t.Fatal(got)
	}
}

func TestRevocationCannotWithdrawPublishedReceiptAndRetainsFailedOwnerUpdate(t *testing.T) {
	for _, published := range []bool{false, true} {
		t.Run(map[bool]string{false: "owner-update-failed", true: "already-published"}[published], func(t *testing.T) {
			s, request, e := importedFixture(t)
			if published {
				if _, err := s.Apply(t.Context(), e.ID); err != nil {
					t.Fatal(err)
				}
			}
			called := false
			err := s.RevokeImportedResult(t.Context(), request, func() error { called = true; return errors.New("owner update interrupted") })
			if err == nil || called == published {
				t.Fatalf("incorrect revocation boundary: %v called=%v", err, called)
			}
			if !published {
				if _, err := s.Apply(t.Context(), e.ID); err == nil {
					t.Fatal("failed owner update restored write authority")
				}
				if err := s.RevokeImportedResult(t.Context(), request, func() error { return nil }); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
