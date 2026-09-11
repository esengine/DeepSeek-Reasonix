package workspacelease

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"reasonix/internal/filelock"
)

func collidingRootOwners(t *testing.T) (string, *Owner, *Owner) {
	t.Helper()
	base, err := CanonicalWorkspace(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	locks := t.TempDir()
	seen := map[string]*Owner{}
	for i := range treeLockStripes + 1 {
		owner, err := New(filepath.Join(base, fmt.Sprintf("root-%d", i)), locks, nil)
		if err != nil {
			t.Fatal(err)
		}
		slot := owner.treeLockPath(owner.canonical)
		if first := seen[slot]; first != nil {
			for _, root := range []string{first.canonical, owner.canonical} {
				if err := os.MkdirAll(root, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			return locks, first, owner
		}
		seen[slot] = owner
	}
	t.Fatal("no tree stripe collision found")
	return "", nil, nil
}

func TestRootGroupCoalescesCollidingTreeStripes(t *testing.T) {
	locks, first, second := collidingRootOwners(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	release, err := HoldWriteRoots(ctx, locks, first.canonical, second.canonical)
	if err != nil {
		t.Fatalf("group waited for its own tree stripe: %v", err)
	}
	defer release()
	for _, owner := range []*Owner{first, second} {
		if unlock, err := filelock.TryAcquire(owner.lockPath); !errors.Is(err, filelock.ErrHeld) {
			if unlock != nil {
				unlock()
			}
			t.Fatalf("group omitted legacy workspace protection: %v", err)
		}
	}
	blocked, stop := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer stop()
	if unlock, err := second.HoldWrite(blocked); !errors.Is(err, context.DeadlineExceeded) {
		if unlock != nil {
			unlock()
		}
		t.Fatalf("independent owner bypassed group: %v", err)
	}
	release()
	release() // Group release is idempotent.
	for _, owner := range []*Owner{first, second} {
		unlock, err := owner.HoldWrite(ctx)
		if err != nil {
			t.Fatalf("group leaked lease: %v", err)
		}
		unlock()
	}
}

func TestRootGroupCancellationReleasesPartialCompatibilityHolds(t *testing.T) {
	locks, first, second := collidingRootOwners(t)
	holdTree, err := filelock.TryAcquire(first.treeLockPath(first.canonical))
	if err != nil {
		t.Fatal(err)
	}
	defer holdTree()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if release, err := HoldWriteRoots(ctx, locks, first.canonical, second.canonical); !errors.Is(err, context.DeadlineExceeded) {
		if release != nil {
			release()
		}
		t.Fatalf("group did not honor cancellation: %v", err)
	}
	for _, owner := range []*Owner{first, second} {
		unlock, err := filelock.TryAcquire(owner.lockPath)
		if err != nil {
			t.Fatalf("cancelled group leaked compatibility lock: %v", err)
		}
		unlock()
	}
}

func TestRootGroupPromotesRequestedAncestorAndOrdersBothDirections(t *testing.T) {
	parent := t.TempDir()
	child := filepath.Join(parent, "child")
	if err := os.Mkdir(child, 0o700); err != nil {
		t.Fatal(err)
	}
	locks := t.TempDir()
	_, forward, err := rootLockDomains(locks, []string{parent, child})
	if err != nil {
		t.Fatal(err)
	}
	_, reverse, err := rootLockDomains(locks, []string{child, parent, parent})
	if err != nil || !reflect.DeepEqual(forward, reverse) {
		t.Fatalf("group ordering depends on caller order: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	release, err := HoldWriteRoots(ctx, locks, child, parent)
	if err != nil {
		t.Fatalf("group reacquired its ancestor: %v", err)
	}
	defer release()
	owner, err := New(parent, locks, nil)
	if err != nil {
		t.Fatal(err)
	}
	if unlock, err := filelock.TryAcquire(owner.lockPath); !errors.Is(err, filelock.ErrHeld) {
		if unlock != nil {
			unlock()
		}
		t.Fatalf("ancestor was not exclusive: %v", err)
	}
}

func TestRootGroupRejectsCancelledContextWithFreeLocks(t *testing.T) {
	root, locks := t.TempDir(), t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if release, err := HoldWriteRoots(ctx, locks, root); !errors.Is(err, context.Canceled) {
		if release != nil {
			release()
		}
		t.Fatalf("cancelled group acquired free locks: %v", err)
	}
}
