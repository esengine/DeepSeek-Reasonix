package workspacelease

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
)

func TestWideningAcquiresAllClaimsOrRefusesWithoutWaiting(t *testing.T) {
	for _, whole := range []bool{false, true} {
		t.Run(map[bool]string{false: "paths", true: "workspace"}[whole], func(t *testing.T) {
			root, locks := pathLeaseRoot(t), testenv.TempDir(t)
			a, _ := New(root, locks, nil)
			b, _ := New(root, locks, nil)
			a.BeginRun()
			b.BeginRun()
			defer a.EndRun()
			defer b.EndRun()
			a.SetHolder(func() string { return "Session A" })
			b.SetHolder(func() string { return "Session B" })
			if err := a.AcquirePaths(context.Background(), []string{"a"}); err != nil {
				t.Fatal(err)
			}
			if err := b.AcquirePaths(context.Background(), []string{"b"}); err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			pathsA, pathsB := []string{"b"}, []string{"a"}
			if whole {
				pathsA, pathsB = nil, nil
			}
			for _, request := range []struct {
				owner *Owner
				paths []string
			}{{a, pathsA}, {b, pathsB}} {
				err := request.owner.AcquirePaths(ctx, request.paths)
				if err == nil || errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("widening must refuse without waiting or dropping earlier claims: %v", err)
				}
			}
			if !a.State().Acquired || !b.State().Acquired {
				t.Fatal("original claims lost")
			}
			b.EndRun()
			if err := a.AcquirePaths(ctx, pathsA); err != nil {
				t.Fatalf("retry after release: %v", err)
			}
		})
	}
}

func TestConflictCarriesHolderAndExtentThroughCancellation(t *testing.T) {
	root, locks := pathLeaseRoot(t), testenv.TempDir(t)
	a, _ := New(root, locks, nil)
	a.SetHolder(func() string { return "Fixture session A" })
	a.BeginRun()
	defer a.EndRun()
	if err := a.AcquirePaths(context.Background(), []string{"owned.txt"}); err != nil {
		t.Fatal(err)
	}
	b, _ := New(root, locks, nil)
	b.BeginRun()
	defer b.EndRun()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	err := b.AcquirePaths(ctx, []string{"owned.txt"})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("cancellation identity: %v", err)
	}
	var coded interface{ RefusalCode() string }
	if !errors.As(err, &coded) || coded.RefusalCode() != "workspace.write_conflict" {
		t.Fatalf("missing typed conflict: %T %v", err, err)
	}
	data, _ := json.Marshal(err)
	if !strings.Contains(string(data), "Fixture session A") || !strings.Contains(string(data), "owned.txt") {
		t.Fatalf("missing attribution: %s", data)
	}
}

func TestWindowsMissingAliasesCannotEstablishDisjointClaims(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows path aliases")
	}
	for _, alias := range []string{"file.", "file ", "dir./file", "dir /file", "SHORT~1/file"} {
		t.Run(alias, func(t *testing.T) {
			root := pathLeaseRoot(t)
			o, _ := New(root, testenv.TempDir(t), nil)
			if got := o.normalizePaths([]string{filepath.Join(root, alias)}); len(got) != 0 {
				t.Fatalf("ambiguous missing component narrowed claim: %v", got)
			}
		})
	}
}

func TestWaitPastGraceClosesAsPairEvenWithoutAnotherRetry(t *testing.T) {
	withWaitGrace(t, time.Millisecond)
	var log waitLog
	o := &Owner{onWait: log.note}
	w := waitClock{owner: o}
	w.contend()
	time.Sleep(5 * time.Millisecond)
	w.close(WaitAcquired)
	if !log.sameAs(WaitBegan, WaitAcquired) {
		t.Fatalf("lost wait pair: %v", log.outcomes())
	}
}

func TestUnnamedClaimsHaveDistinctStableSessionIdentities(t *testing.T) {
	root, locks := pathLeaseRoot(t), testenv.TempDir(t)
	a, _ := New(root, locks, nil)
	b, _ := New(root, locks, nil)
	a.BeginRun()
	b.BeginRun()
	defer a.EndRun()
	defer b.EndRun()
	if err := a.AcquirePaths(context.Background(), []string{"a"}); err != nil {
		t.Fatal(err)
	}
	if err := b.AcquirePaths(context.Background(), []string{"b"}); err != nil {
		t.Fatal(err)
	}
	if a.sessionID() == "" || b.sessionID() == "" || a.sessionID() == b.sessionID() {
		t.Fatal("unnamed sessions cannot be distinguished")
	}
	first := a.sessionID()
	if err := a.AcquirePaths(context.Background(), []string{"c"}); err != nil {
		t.Fatal(err)
	}
	if a.sessionID() != first {
		t.Fatal("session identity changed during widening")
	}
	a.SetSessionID(func() string { return "persisted-session" })
	if a.sessionID() != "persisted-session" {
		t.Fatal("persisted identity was not preferred")
	}
}
