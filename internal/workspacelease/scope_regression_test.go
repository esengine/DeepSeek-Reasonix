package workspacelease

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/filelock"
)

func TestReversePathBatchesSerializeWithoutDeadlock(t *testing.T) {
	root, locks := t.TempDir(), t.TempDir()
	first, _ := New(root, locks, nil)
	second, _ := New(root, locks, nil)
	a := filepath.Join(root, "a.go")
	b := filepath.Join(root, "b.go")
	start := make(chan struct{})
	type result struct {
		release func()
		err     error
	}
	results := make(chan result, 2)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	for owner, paths := range map[*Owner][]string{first: {a, b}, second: {b, a}} {
		go func() {
			<-start
			release, err := owner.HoldWriteForPaths(ctx, paths)
			results <- result{release: release, err: err}
		}()
	}
	close(start)

	one := <-results
	if one.err != nil {
		t.Fatalf("first batch acquire: %v", one.err)
	}
	one.release()
	two := <-results
	if two.err != nil {
		t.Fatalf("reverse batch acquire: %v", two.err)
	}
	two.release()
}

func TestExclusiveUpgradeKeepsActiveFileProtected(t *testing.T) {
	root, locks := t.TempDir(), t.TempDir()
	owner, _ := New(root, locks, nil)
	contender, _ := New(root, locks, nil)
	path := filepath.Join(root, "protected.go")
	releasePath, err := owner.HoldWriteForPath(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}

	upgrade := make(chan error, 1)
	go func() {
		release, err := owner.HoldWrite(context.Background())
		if err == nil {
			release()
		}
		upgrade <- err
	}()
	waitForOwnerAcquisition(t, owner)

	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	_, err = contender.HoldWriteForPath(ctx, path)
	cancel()
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("active file lost protection during upgrade: %v", err)
	}
	releasePath()
	select {
	case err := <-upgrade:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("exclusive upgrade did not continue after the file hold released")
	}
}

func TestUncontendedPathWriteDoesNotNotifyWait(t *testing.T) {
	var notices atomic.Int32
	owner, err := New(t.TempDir(), t.TempDir(), func() { notices.Add(1) })
	if err != nil {
		t.Fatal(err)
	}
	release, err := owner.HoldWriteForPath(context.Background(), filepath.Join(owner.canonical, "a.go"))
	if err != nil {
		t.Fatal(err)
	}
	release()
	if got := notices.Load(); got != 0 {
		t.Fatalf("uncontended path write emitted %d wait notices", got)
	}
}

func TestWorkspaceWriterGetsPriorityOverNewPathReader(t *testing.T) {
	root, locks := t.TempDir(), t.TempDir()
	reader, _ := New(root, locks, nil)
	writerWaiting := make(chan struct{}, 1)
	writer, _ := New(root, locks, func() { writerWaiting <- struct{}{} })
	lateReader, _ := New(root, locks, nil)
	releaseReader, err := reader.HoldWriteForPath(context.Background(), filepath.Join(root, "a.go"))
	if err != nil {
		t.Fatal(err)
	}
	type result struct {
		release func()
		err     error
	}
	writerResult := make(chan result, 1)
	go func() {
		release, acquireErr := writer.HoldWrite(context.Background())
		writerResult <- result{release: release, err: acquireErr}
	}()
	select {
	case <-writerWaiting:
	case <-time.After(2 * time.Second):
		t.Fatal("workspace writer did not report contention")
	}
	lateResult := make(chan result, 1)
	go func() {
		release, acquireErr := lateReader.HoldWriteForPath(context.Background(), filepath.Join(root, "b.go"))
		lateResult <- result{release: release, err: acquireErr}
	}()
	releaseReader()
	acquiredWriter := <-writerResult
	if acquiredWriter.err != nil {
		t.Fatal(acquiredWriter.err)
	}
	select {
	case late := <-lateResult:
		if late.release != nil {
			late.release()
		}
		t.Fatal("new path reader bypassed the waiting workspace writer")
	default:
	}
	acquiredWriter.release()
	late := <-lateResult
	if late.err != nil {
		t.Fatal(late.err)
	}
	late.release()
}

func TestSameOwnerReusesSharedDomainsWithoutLettingOtherReadersBypassWriter(t *testing.T) {
	root, locks := t.TempDir(), t.TempDir()
	owner, _ := New(root, locks, nil)
	firstPath, secondPath := increasingPathSlots(t, owner)
	releaseFirst, err := owner.HoldWriteForPath(context.Background(), firstPath)
	if err != nil {
		t.Fatal(err)
	}

	writerWaiting := make(chan struct{}, 1)
	writer, _ := New(root, locks, func() { writerWaiting <- struct{}{} })
	writerResult := make(chan holdResult, 1)
	go func() {
		release, acquireErr := writer.HoldWrite(context.Background())
		writerResult <- holdResult{release: release, err: acquireErr}
	}()
	select {
	case <-writerWaiting:
	case <-time.After(2 * time.Second):
		t.Fatal("workspace writer did not report waiting")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	releaseSecond, err := owner.HoldWriteForPath(ctx, secondPath)
	cancel()
	if err != nil {
		releaseFirst()
		t.Fatalf("same owner deadlocked behind a writer waiting on its first path hold: %v", err)
	}

	lateWaiting := make(chan struct{}, 1)
	lateReader, _ := New(root, locks, func() { lateWaiting <- struct{}{} })
	lateResult := make(chan holdResult, 1)
	go func() {
		release, acquireErr := lateReader.HoldWriteForPath(context.Background(), secondPath)
		lateResult <- holdResult{release: release, err: acquireErr}
	}()
	select {
	case <-lateWaiting:
	case <-time.After(2 * time.Second):
		t.Fatal("different owner did not queue behind the waiting workspace writer")
	}

	releaseSecond()
	releaseFirst()
	acquiredWriter := awaitHold(t, writerResult)
	select {
	case late := <-lateResult:
		if late.release != nil {
			late.release()
		}
		t.Fatalf("different owner bypassed the waiting workspace writer: %v", late.err)
	default:
	}
	acquiredWriter.release()
	acquiredLate := awaitHold(t, lateResult)
	acquiredLate.release()
}

func increasingPathSlots(t *testing.T, owner *Owner) (string, string) {
	t.Helper()
	first := filepath.Join(owner.canonical, "slot-0.go")
	firstSlot := canonicalPathSlot(t, owner, first)
	for i := 1; i < pathLockStripes*2; i++ {
		candidate := filepath.Join(owner.canonical, fmt.Sprintf("slot-%d.go", i))
		candidateSlot := canonicalPathSlot(t, owner, candidate)
		if candidateSlot > firstSlot {
			return first, candidate
		}
		if candidateSlot < firstSlot {
			return candidate, first
		}
	}
	t.Fatal("could not find two distinct path lock slots")
	return "", ""
}

func canonicalPathSlot(t *testing.T, owner *Owner, path string) string {
	t.Helper()
	specs, err := owner.pathSpecs([]string{path})
	if err != nil || len(specs) != 1 {
		t.Fatalf("resolve path slot for %q: specs=%v err=%v", path, specs, err)
	}
	return specs[0].slot
}

func TestPathLockFilesUseBoundedStripes(t *testing.T) {
	owner, err := New(t.TempDir(), t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for i := range pathLockStripes * 3 {
		seen[owner.pathLockPath(fmt.Sprintf("file-%d", i))] = true
	}
	if len(seen) > pathLockStripes {
		t.Fatalf("path lock files = %d, want at most %d", len(seen), pathLockStripes)
	}
}

func TestHierarchyLockFilesUseBoundedStripes(t *testing.T) {
	owner, err := New(t.TempDir(), t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for i := range treeLockStripes * 3 {
		seen[owner.treeLockPath(fmt.Sprintf("directory-%d", i))] = true
	}
	if len(seen) > treeLockStripes {
		t.Fatalf("hierarchy lock files = %d, want at most %d", len(seen), treeLockStripes)
	}
}

func TestParentPathAndNestedWorkspaceWriterSerializeBothDirections(t *testing.T) {
	parent, locks := t.TempDir(), t.TempDir()
	nested := filepath.Join(parent, "nested")
	if err := os.MkdirAll(filepath.Join(nested, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(nested, "shared.go")

	t.Run("parent path blocks nested workspace", func(t *testing.T) {
		parentOwner, err := New(parent, locks, nil)
		if err != nil {
			t.Fatal(err)
		}
		waiting := make(chan struct{}, 1)
		nestedOwner, err := New(nested, locks, func() { waiting <- struct{}{} })
		if err != nil {
			t.Fatal(err)
		}
		releasePath, err := parentOwner.HoldWriteForPath(context.Background(), path)
		if err != nil {
			t.Fatal(err)
		}
		result := make(chan holdResult, 1)
		go func() {
			release, acquireErr := nestedOwner.HoldWrite(context.Background())
			result <- holdResult{release: release, err: acquireErr}
		}()
		assertHoldWaiting(t, waiting, result)
		releasePath()
		acquired := awaitHold(t, result)
		acquired.release()
	})

	t.Run("nested workspace blocks parent path", func(t *testing.T) {
		nestedOwner, err := New(nested, locks, nil)
		if err != nil {
			t.Fatal(err)
		}
		waiting := make(chan struct{}, 1)
		parentOwner, err := New(parent, locks, func() { waiting <- struct{}{} })
		if err != nil {
			t.Fatal(err)
		}
		releaseWorkspace, err := nestedOwner.HoldWrite(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		result := make(chan holdResult, 1)
		go func() {
			release, acquireErr := parentOwner.HoldWriteForPath(context.Background(), path)
			result <- holdResult{release: release, err: acquireErr}
		}()
		assertHoldWaiting(t, waiting, result)
		releaseWorkspace()
		acquired := awaitHold(t, result)
		acquired.release()
	})
}

func TestHierarchyLocksDoNotSerializeUnrelatedWorkspaces(t *testing.T) {
	locks := t.TempDir()
	base := t.TempDir()
	firstRoot := filepath.Join(base, "workspace-a")
	if err := os.Mkdir(firstRoot, 0o700); err != nil {
		t.Fatal(err)
	}
	first, err := New(firstRoot, locks, nil)
	if err != nil {
		t.Fatal(err)
	}
	secondRoot, secondPath := unrelatedTreePath(t, first, base)
	second, err := New(secondRoot, locks, nil)
	if err != nil {
		t.Fatal(err)
	}
	releaseWorkspace, err := first.HoldWrite(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer releaseWorkspace()
	releasePath, err := second.HoldWriteForPath(context.Background(), secondPath)
	if err != nil {
		t.Fatalf("unrelated path write was serialized: %v", err)
	}
	releasePath()
}

func unrelatedTreePath(t *testing.T, blocker *Owner, base string) (string, string) {
	t.Helper()
	blockedSlot := blocker.treeLockPath(blocker.canonical)
	for i := 1; i < treeLockStripes*2; i++ {
		root := filepath.Join(base, fmt.Sprintf("workspace-%d", i))
		path := filepath.Join(root, "b.go")
		if blocker.treeLockPath(root) == blockedSlot || blocker.treeLockPath(path) == blockedSlot {
			continue
		}
		if err := os.Mkdir(root, 0o700); err != nil {
			t.Fatal(err)
		}
		return root, path
	}
	t.Fatal("could not find unrelated hierarchy lock slots")
	return "", ""
}

func TestHierarchyProtocolIntersectsOldExactWorkspaceLocks(t *testing.T) {
	parent, locks := t.TempDir(), t.TempDir()
	nested := filepath.Join(parent, "nested")
	if err := os.MkdirAll(filepath.Join(nested, ".git"), 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(nested, "shared.go")
	parentOwner, _ := New(parent, locks, nil)
	nestedOwner, _ := New(nested, locks, nil)

	t.Run("old parent blocks new nested workspace", func(t *testing.T) {
		releaseOld, err := filelock.TryAcquire(parentOwner.lockPath)
		if err != nil {
			t.Fatal(err)
		}
		waiting := make(chan struct{}, 1)
		contender, _ := New(nested, locks, func() { waiting <- struct{}{} })
		result := make(chan holdResult, 1)
		go func() {
			release, acquireErr := contender.HoldWrite(context.Background())
			result <- holdResult{release: release, err: acquireErr}
		}()
		assertHoldWaiting(t, waiting, result)
		releaseOld()
		acquired := awaitHold(t, result)
		acquired.release()
	})

	t.Run("old nested git root blocks new parent path", func(t *testing.T) {
		releaseOld, err := filelock.TryAcquire(nestedOwner.lockPath)
		if err != nil {
			t.Fatal(err)
		}
		waiting := make(chan struct{}, 1)
		contender, _ := New(parent, locks, func() { waiting <- struct{}{} })
		result := make(chan holdResult, 1)
		go func() {
			release, acquireErr := contender.HoldWriteForPath(context.Background(), path)
			result <- holdResult{release: release, err: acquireErr}
		}()
		assertHoldWaiting(t, waiting, result)
		releaseOld()
		acquired := awaitHold(t, result)
		acquired.release()
	})
}

func TestLeaseStatesOverlapUsesKeysAcrossWorkspaceRoots(t *testing.T) {
	parent := t.TempDir()
	nested := filepath.Join(parent, "nested")
	inside := filepath.Join(nested, "inside.go")
	outside := filepath.Join(parent, "outside.go")
	waitingFile := State{Scope: "file", WaitingKeys: []string{inside}}
	holderFile := State{HeldScope: "file", HeldKeys: []string{inside}}
	if !LeaseStatesOverlap(parent, waitingFile, nested, holderFile) {
		t.Fatal("identical file keys must overlap across canonical roots")
	}
	if LeaseStatesOverlap(nested, State{Scope: "workspace", WaitingKeys: []string{nested}}, parent, State{
		HeldScope: "file", HeldKeys: []string{outside},
	}) {
		t.Fatal("nested whole workspace must not match a parent path outside it")
	}
	if !LeaseStatesOverlap(nested, State{Scope: "workspace", WaitingKeys: []string{nested}}, parent, holderFile) {
		t.Fatal("nested whole workspace must match a parent path inside it")
	}
	if !LeaseStatesOverlap(parent, waitingFile, nested, State{
		HeldScope: "workspace", HeldKeys: []string{nested},
	}) {
		t.Fatal("nested whole holder must match a parent path inside it")
	}
}

type holdResult struct {
	release func()
	err     error
}

func assertHoldWaiting(t *testing.T, waiting <-chan struct{}, result <-chan holdResult) {
	t.Helper()
	select {
	case <-waiting:
	case acquired := <-result:
		if acquired.release != nil {
			acquired.release()
		}
		t.Fatalf("conflicting hold acquired before release: %v", acquired.err)
	case <-time.After(2 * time.Second):
		t.Fatal("conflicting hold did not report waiting")
	}
	select {
	case acquired := <-result:
		if acquired.release != nil {
			acquired.release()
		}
		t.Fatalf("conflicting hold acquired while blocker remained: %v", acquired.err)
	default:
	}
}

func awaitHold(t *testing.T, result <-chan holdResult) holdResult {
	t.Helper()
	select {
	case acquired := <-result:
		if acquired.err != nil {
			t.Fatal(acquired.err)
		}
		return acquired
	case <-time.After(2 * time.Second):
		t.Fatal("hold did not acquire after blocker released")
		return holdResult{}
	}
}

func TestNestedWorkspaceRootsSharePathStripes(t *testing.T) {
	root, locks := t.TempDir(), t.TempDir()
	child := filepath.Join(root, "child")
	if err := os.Mkdir(child, 0o700); err != nil {
		t.Fatal(err)
	}
	parentOwner, _ := New(root, locks, nil)
	childOwner, _ := New(child, locks, nil)
	path := filepath.Join(child, "shared.go")
	release, err := parentOwner.HoldWriteForPath(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	defer release()
	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	if _, err := childOwner.HoldWriteForPath(ctx, path); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("nested workspace bypassed the same-file stripe: %v", err)
	}
}

func TestRetainedPathHoldCanBeReused(t *testing.T) {
	owner, err := New(t.TempDir(), t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(owner.canonical, "retained.go")
	release, err := owner.HoldWriteForPath(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	owner.RetainUntil(done)
	release()
	reused, err := owner.HoldWriteForPath(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}
	reused()
	close(done)
}

func TestDarwinCaseAliasesShareFileKey(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS case-insensitive alias regression")
	}
	root := t.TempDir()
	actual := filepath.Join(root, "CaseAlias.go")
	if err := os.WriteFile(actual, []byte("package alias"), 0o600); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(root, "casealias.go")
	actualKey, _, err := canonicalFileKey(actual)
	if err != nil {
		t.Fatal(err)
	}
	aliasKey, _, err := canonicalFileKey(alias)
	if err != nil {
		t.Fatal(err)
	}
	if actualKey != aliasKey {
		t.Fatalf("case aliases produced different keys: %q != %q", actualKey, aliasKey)
	}
}

func waitForOwnerAcquisition(t *testing.T, owner *Owner) {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		owner.mu.Lock()
		acquiring := owner.lease.acquiring
		changed := owner.lease.changed
		owner.mu.Unlock()
		if acquiring {
			return
		}
		select {
		case <-changed:
		case <-deadline:
			t.Fatal("owner did not begin acquisition")
		}
	}
}
