package gitpush

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPushTransferUsesIndependentObjectsAndShortSourceScopes(t *testing.T) {
	f := newFixture(t)
	plan, err := Preview(t.Context(), f.repo)
	if err != nil {
		t.Fatal(err)
	}
	objects := filepath.Join(f.repo.CommonDir, "objects")
	hidden := filepath.Join(t.TempDir(), "source-objects")
	var acquired, released int
	scope := func(context.Context) (func(), error) {
		acquired++
		if acquired == 2 {
			if f.remoteHead(t) != plan.Head {
				t.Fatal("settlement started before the remote accepted the exact commit")
			}
			if err := os.Rename(hidden, objects); err != nil {
				t.Fatal(err)
			}
		}
		return func() {
			released++
			if released == 1 {
				if err := os.Rename(objects, hidden); err != nil {
					t.Fatal(err)
				}
			}
		}, nil
	}
	t.Cleanup(func() {
		if _, err := os.Stat(hidden); err == nil {
			if err := os.Rename(hidden, objects); err != nil {
				t.Error(err)
			}
		}
	})
	result, err := PushWithScope(t.Context(), f.repo, plan, scope)
	if err != nil || result.TrackingStatus != "updated" {
		t.Fatalf("push depended on source objects during network transfer: %+v %v", result, err)
	}
	if acquired != 2 || released != 2 {
		t.Fatalf("source scopes: acquired=%d released=%d", acquired, released)
	}
}

func TestPushPreservesConcurrentTrackingUpdateAndReportsRemoteSuccess(t *testing.T) {
	for _, refusal := range []string{"changed", "unavailable"} {
		t.Run(refusal, func(t *testing.T) {
			f := newFixture(t)
			plan, err := Preview(t.Context(), f.repo)
			if err != nil {
				t.Fatal(err)
			}
			newHead := run(t, f.work, "commit-tree", "HEAD^{tree}", "-p", "HEAD", "-m", "concurrent fetch")
			var calls int
			scope := func(context.Context) (func(), error) {
				calls++
				if calls == 2 {
					if refusal == "unavailable" {
						return nil, errors.New("source is unavailable")
					}
					run(t, f.work, "update-ref", trackingRef(plan), newHead)
				}
				return func() {}, nil
			}
			result, err := PushWithScope(t.Context(), f.repo, plan, scope)
			if err != nil || result.TrackingStatus != "not_updated" || f.remoteHead(t) != plan.Head {
				t.Fatalf("remote success was lost: %+v %v", result, err)
			}
			if refusal == "changed" && run(t, f.work, "rev-parse", trackingRef(plan)) != newHead {
				t.Fatal("settlement replaced a concurrent tracking update")
			}
		})
	}
}

func TestPushRefusedPreparationDoesNotReachRemote(t *testing.T) {
	f := newFixture(t)
	plan, err := Preview(t.Context(), f.repo)
	if err != nil {
		t.Fatal(err)
	}
	before := f.remoteHead(t)
	_, err = PushWithScope(t.Context(), f.repo, plan, func(context.Context) (func(), error) {
		return nil, context.Canceled
	})
	if !errors.Is(err, context.Canceled) || f.remoteHead(t) != before {
		t.Fatalf("refused preparation reached remote: %v", err)
	}
}

func TestPushTransferCarriesOnlyWhatTheRemoteLacks(t *testing.T) {
	f := newFixture(t)
	write(t, f.work, "big.bin", strings.Repeat("remote history ", 1<<16))
	run(t, f.work, "add", ".")
	run(t, f.work, "commit", "-qm", "remote-known")
	big := run(t, f.work, "rev-parse", "HEAD:big.bin")
	run(t, f.work, "push", "-q", "origin", "main")
	run(t, f.work, "checkout", "-q", "-b", "side", "HEAD~2")
	write(t, f.work, "side.go", "package side\n")
	run(t, f.work, "add", ".")
	run(t, f.work, "commit", "-qm", "side")
	run(t, f.work, "checkout", "-q", "main")
	run(t, f.work, "merge", "-q", "--no-ff", "-m", "merge older side", "side")
	plan, err := Preview(t.Context(), f.repo)
	if err != nil {
		t.Fatal(err)
	}
	prepared, cleanup, err := preparePush(t.Context(), f.repo, plan, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if err := prepared.scratch.repo.Command(t.Context(), "cat-file", "-e", big).Run(); err == nil {
		t.Fatal("push copied history the remote already holds")
	}
	if _, err := PushWithScope(t.Context(), f.repo, plan, nil); err != nil {
		t.Fatal(err)
	}
	if f.remoteHead(t) != f.workHead(t) {
		t.Fatal("remote did not receive the merge")
	}
	run(t, f.remote, "fsck", "--strict")
}

func TestNewBranchTransferExcludesHistoryTheRemoteHolds(t *testing.T) {
	f := newFixture(t)
	write(t, f.work, "big.bin", strings.Repeat("remote history ", 1<<16))
	run(t, f.work, "add", ".")
	run(t, f.work, "commit", "-qm", "remote-known")
	big := run(t, f.work, "rev-parse", "HEAD:big.bin")
	run(t, f.work, "push", "-q", "origin", "main")
	run(t, f.work, "checkout", "-q", "-b", "feature")
	write(t, f.work, "feature.go", "package feature\n")
	run(t, f.work, "add", ".")
	run(t, f.work, "commit", "-qm", "feature")
	run(t, f.work, "config", "branch.feature.remote", "origin")
	run(t, f.work, "config", "branch.feature.merge", "refs/heads/feature")
	plan, err := Preview(t.Context(), f.repo)
	if err != nil {
		t.Fatal(err)
	}
	prepared, cleanup, err := preparePush(t.Context(), f.repo, plan, nil, false)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if err := prepared.scratch.repo.Command(t.Context(), "cat-file", "-e", big).Run(); err == nil {
		t.Fatal("a new branch copied history the remote already holds")
	}
	if _, err := PushWithScope(t.Context(), f.repo, plan, nil); err != nil {
		t.Fatal(err)
	}
	if run(t, f.remote, "rev-parse", "feature") != f.workHead(t) {
		t.Fatal("remote did not receive the new branch")
	}
	run(t, f.remote, "fsck", "--strict")
}
