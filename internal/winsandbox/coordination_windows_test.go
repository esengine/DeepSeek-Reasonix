//go:build windows

package winsandbox

import (
	"bytes"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// lockNoticeBuffer is an io.Writer safe to read while the contender goroutine
// writes the queue notice into it.
type lockNoticeBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *lockNoticeBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *lockNoticeBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func TestWindowsRootLockQueueEmitsNotice(t *testing.T) {
	root := t.TempDir()
	// Long enough for the contender to sit through one full silent slice
	// (windowsRootLockNoticeAfter) and emit the queue notice before timing out.
	t.Setenv("WINDOWS_SANDBOX_LOCK_MS", "5000")
	held, err := lockWindowsRoots([]string{root}, nil, "", 0)
	if err != nil {
		t.Fatalf("hold lock: %v", err)
	}

	var notice lockNoticeBuffer
	done := make(chan struct{})
	go func() {
		defer close(done)
		lock, err := lockWindowsRoots([]string{root}, &notice, "", 0)
		if err == nil {
			lock.release()
		}
	}()

	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(notice.String(), "waiting for another sandboxed command") {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !strings.Contains(notice.String(), "waiting for another sandboxed command") {
		t.Fatalf("queued lock never emitted a notice; got %q", notice.String())
	}
	held.release()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("contender never finished after release")
	}
}

// A held root lock records its holder (pid + command label); a queued
// contender's notice names that command so the user knows what to stop, and
// release removes the record again.
func TestWindowsRootLockHolderRecordedAndCleared(t *testing.T) {
	root := t.TempDir()
	t.Setenv("WINDOWS_SANDBOX_LOCK_MS", "5000")
	held, err := lockWindowsRoots([]string{root}, nil, "npm run dev", 0)
	if err != nil {
		t.Fatalf("hold lock: %v", err)
	}
	name := windowsRootLockNames([]string{root})[0]
	data, err := os.ReadFile(windowsLockHolderPath(name))
	if err != nil {
		t.Fatalf("holder record not written: %v", err)
	}
	rec, ok := parseLockHolderRecord(string(data))
	if !ok || rec.pid != os.Getpid() || rec.label != "npm run dev" {
		t.Fatalf("holder record = %+v ok=%v, want this process with label", rec, ok)
	}

	var notice lockNoticeBuffer
	done := make(chan struct{})
	go func() {
		defer close(done)
		lock, err := lockWindowsRoots([]string{root}, &notice, "", 0)
		if err == nil {
			lock.release()
		}
	}()
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(notice.String(), `held by "npm run dev"`) {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !strings.Contains(notice.String(), `held by "npm run dev"`) {
		t.Fatalf("queued notice never named the holder; got %q", notice.String())
	}
	held.release()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("contender never finished after release")
	}
	if pathExists(windowsLockHolderPath(name)) {
		t.Fatal("holder record not removed after release")
	}
}

func TestWindowsRootLockNamesAreSortedAndDeduped(t *testing.T) {
	names := windowsRootLockNames([]string{
		`C:\work\b`,
		`C:\work\a`,
		`C:\WORK\A`, // same as a, different case
		"",
		".",
	})
	if len(names) != 2 {
		t.Fatalf("lock names = %v, want 2 distinct roots", names)
	}
	if !sort.StringsAreSorted(names) {
		t.Fatalf("lock names must be sorted for deadlock-free acquisition: %v", names)
	}
	for _, n := range names {
		if !strings.HasPrefix(n, `Local\windows-sandbox.`) {
			t.Fatalf("unexpected lock name %q", n)
		}
	}
	// Case-insensitive dedup: A and a must collapse to one name.
	same := windowsRootLockNames([]string{`C:\work\a`})
	if len(same) != 1 || !contains(names, same[0]) {
		t.Fatalf("case-insensitive dedup broken: %v vs %v", names, same)
	}
}

func TestWindowsRootLockSerializesSameRoot(t *testing.T) {
	root := t.TempDir()
	// The two locks target the same root, so the second acquire must block until
	// the first releases. Use a short timeout so a regression fails fast.
	t.Setenv("WINDOWS_SANDBOX_LOCK_MS", "2000")

	first, err := lockWindowsRoots([]string{root}, nil, "", 0)
	if err != nil {
		t.Fatalf("first lock: %v", err)
	}

	acquired := make(chan *windowsRootLock, 1)
	go func() {
		second, err := lockWindowsRoots([]string{root}, nil, "", 0)
		if err != nil {
			acquired <- nil
			return
		}
		acquired <- second
	}()

	select {
	case <-acquired:
		t.Fatal("second lock acquired while first was held; roots not serialized")
	case <-time.After(300 * time.Millisecond):
		// Expected: still blocked.
	}

	first.release()
	select {
	case second := <-acquired:
		if second == nil {
			t.Fatal("second lock failed to acquire after first released")
		}
		second.release()
	case <-time.After(3 * time.Second):
		t.Fatal("second lock never acquired after first released")
	}
}

func TestWindowsRootLockTimesOutWhenHeld(t *testing.T) {
	root := t.TempDir()
	t.Setenv("WINDOWS_SANDBOX_LOCK_MS", "300")
	held, err := lockWindowsRoots([]string{root}, nil, "", 0)
	if err != nil {
		t.Fatalf("hold lock: %v", err)
	}
	defer held.release()

	// The contender must run on its own goroutine, not the holder's: a Windows
	// mutex is recursive for its owning OS thread, so re-acquiring from the same
	// thread would succeed instead of timing out. The holder pins its thread, so
	// the contender goroutine lands on a different thread and genuinely blocks.
	type result struct {
		err     error
		elapsed time.Duration
	}
	done := make(chan result, 1)
	go func() {
		start := time.Now()
		lock, err := lockWindowsRoots([]string{root}, nil, "", 0)
		if lock != nil {
			lock.release()
		}
		done <- result{err: err, elapsed: time.Since(start)}
	}()

	select {
	case r := <-done:
		if r.err == nil {
			t.Fatal("expected timeout acquiring a held lock")
		}
		if !strings.Contains(r.err.Error(), "timed out") {
			t.Fatalf("error = %v, want timeout", r.err)
		}
		if r.elapsed > 2*time.Second {
			t.Fatalf("lock timeout took too long: %s", r.elapsed)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("contender never returned")
	}
}

func TestWindowsRootLockMultiRootNoSelfDeadlock(t *testing.T) {
	a := t.TempDir()
	b := t.TempDir()
	t.Setenv("WINDOWS_SANDBOX_LOCK_MS", "2000")
	// Acquiring multiple roots in one call must not deadlock regardless of the
	// order the caller passes them, because names are sorted internally.
	lock, err := lockWindowsRoots([]string{b, a, b}, nil, "", 0)
	if err != nil {
		t.Fatalf("multi-root lock: %v", err)
	}
	lock.release()
}

func TestWindowsResidueMarkerIncrementalRoundTrip(t *testing.T) {
	// Redirect the marker dir into a temp dir so the test never touches the real
	// %TEMP%. windowsDenyMarkerDir derives from os.TempDir, so overriding TMP is
	// enough on Windows.
	tmp := t.TempDir()
	t.Setenv("TMP", tmp)
	t.Setenv("TEMP", tmp)

	// Append entries one at a time, as the run does before applying each ACE.
	run := newWindowsDenyResidueRun()
	defer run.clear()
	if err := run.recordBeforeApply(residueDeny, `C:\Users\me\.ssh`); err != nil {
		t.Fatalf("record deny: %v", err)
	}
	if err := run.recordBeforeApply(residueGrant, `C:\Users\me\tools`); err != nil {
		t.Fatalf("record grant: %v", err)
	}

	marker := run.marker
	if !pathExists(marker) {
		t.Fatalf("marker not written at %s", marker)
	}
	got := readResidueMarker(marker)
	want := []residueEntry{
		{kind: residueDeny, path: `C:\Users\me\.ssh`},
		{kind: residueGrant, path: `C:\Users\me\tools`},
	}
	if len(got) != len(want) {
		t.Fatalf("marker round-trip = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("marker[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}

	run.clear()
	if pathExists(marker) {
		t.Fatal("marker not cleared")
	}
}

func TestWindowsResidueMarkerSkipsMalformedLines(t *testing.T) {
	// A corrupt or unrecognized marker line must be skipped, never guessed at, so
	// a damaged marker cannot drive a wrong ACE removal.
	tmp := t.TempDir()
	t.Setenv("TMP", tmp)
	t.Setenv("TEMP", tmp)
	if err := os.MkdirAll(windowsDenyMarkerDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	marker := windowsDenyMarkerPath()
	content := "deny\tC:\\Users\\me\\.ssh\n" + // valid
		"garbage-no-tab\n" + // no separator
		"boguskind\tC:\\x\n" + // unknown kind
		"grant\t\n" + // empty path
		"grant\tC:\\Users\\me\\my tools\n" // valid, path with a space
	if err := os.WriteFile(marker, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	got := readResidueMarker(marker)
	want := []residueEntry{
		{kind: residueDeny, path: `C:\Users\me\.ssh`},
		{kind: residueGrant, path: `C:\Users\me\my tools`},
	}
	if len(got) != len(want) {
		t.Fatalf("parsed = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("entry %d = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestWindowsResidueMarkerWriteFailurePropagates(t *testing.T) {
	// If the marker cannot be created, recordResidueBeforeApply must return an
	// error so the caller fails closed instead of applying an untracked ACE. Point
	// TMP at a path that is a file, so MkdirAll of the marker dir underneath it
	// fails.
	tmp := t.TempDir()
	fileAsTemp := filepath.Join(tmp, "not-a-dir")
	if err := os.WriteFile(fileAsTemp, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMP", fileAsTemp)
	t.Setenv("TEMP", fileAsTemp)
	run := newWindowsDenyResidueRun()
	defer run.clear()
	if err := run.recordBeforeApply(residueDeny, `C:\Users\me\.ssh`); err == nil {
		t.Fatal("expected error when the marker directory cannot be created")
	}
}

func TestWindowsResidueMarkerPIDReuseLifecycle(t *testing.T) {
	// The marker path is keyed by PID alone and Windows reuses PIDs, so a file
	// at our own path is not necessarily ours. A run must not delete a
	// predecessor's marker as if it were its own (that orphans the recorded
	// residue forever), and the run-start sweep must consume it rather than
	// skip it as "self".
	tmp := t.TempDir()
	t.Setenv("TMP", tmp)
	t.Setenv("TEMP", tmp)
	if err := os.MkdirAll(windowsDenyMarkerDir(), 0o700); err != nil {
		t.Fatal(err)
	}

	// A marker at our PID's path that this process never wrote models the dead
	// predecessor. The recorded path deliberately does not exist so consuming
	// it exercises only the marker lifecycle, not icacls.
	stale := windowsDenyMarkerPath()
	if err := os.WriteFile(stale, []byte("grant\t"+filepath.Join(tmp, "gone-tool-dir")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Cleanup must leave a marker this process did not write.
	run := newWindowsDenyResidueRun()
	run.clear()
	if !pathExists(stale) {
		t.Fatal("clear removed a marker this process never wrote, orphaning the predecessor's residue")
	}

	// The sweep must consume it instead of skipping pid == self.
	sweepWindowsDenyResidue()
	if pathExists(stale) {
		t.Fatal("sweep skipped a dead predecessor's marker at our own PID path")
	}

	// Once this process records, the marker is owned: the sweep must leave it
	// alone and cleanup must remove it.
	run = newWindowsDenyResidueRun()
	if err := run.recordBeforeApply(residueDeny, filepath.Join(tmp, "own-root")); err != nil {
		t.Fatalf("record own entry: %v", err)
	}
	sweepWindowsDenyResidue()
	if !pathExists(run.marker) {
		t.Fatal("sweep removed this process's live marker")
	}
	run.clear()
	if pathExists(run.marker) {
		t.Fatal("clear did not remove this process's own marker")
	}
}

func TestWindowsResidueRecordDoesNotMixWithStalePredecessorMarker(t *testing.T) {
	// If recording starts while a dead predecessor's same-PID marker is still
	// present (a caller that records without sweeping first), this run must use a
	// distinct marker. Mixing the two runs' lines would let this run's cleanup
	// delete the predecessor's record while only this run's ACEs had been removed.
	tmp := t.TempDir()
	t.Setenv("TMP", tmp)
	t.Setenv("TEMP", tmp)
	if err := os.MkdirAll(windowsDenyMarkerDir(), 0o700); err != nil {
		t.Fatal(err)
	}

	stale := windowsDenyMarkerPath()
	if err := os.WriteFile(stale, []byte("grant\t"+filepath.Join(tmp, "predecessor-tool-dir")+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	run := newWindowsDenyResidueRun()
	defer run.clear()
	ownRoot := filepath.Join(tmp, "own-root")
	if err := run.recordBeforeApply(residueDeny, ownRoot); err != nil {
		t.Fatalf("record own entry: %v", err)
	}
	got := readResidueMarker(run.marker)
	want := []residueEntry{{kind: residueDeny, path: ownRoot}}
	if len(got) != 1 || got[0] != want[0] {
		t.Fatalf("marker after first record = %v, want only this run's entry %v", got, want)
	}
	if !pathExists(stale) {
		t.Fatal("this run removed the predecessor's marker")
	}
	run.clear()
	if pathExists(run.marker) {
		t.Fatal("clear did not remove this process's own marker")
	}
	if !pathExists(stale) {
		t.Fatal("clear removed the predecessor's marker")
	}
	sweepWindowsDenyResidue()
	if pathExists(stale) {
		t.Fatal("sweep did not consume the stale predecessor marker")
	}
}

func TestWindowsResidueConcurrentRunsOwnDistinctMarkers(t *testing.T) {
	// Concurrent Run calls in one Go process share a PID but must not share one
	// marker: the first run to clean up would otherwise delete the second run's
	// residue record while the second run is still live.
	tmp := t.TempDir()
	t.Setenv("TMP", tmp)
	t.Setenv("TEMP", tmp)

	a := newWindowsDenyResidueRun()
	b := newWindowsDenyResidueRun()
	defer a.clear()
	defer b.clear()
	if a.marker == b.marker {
		t.Fatalf("concurrent runs share marker %s", a.marker)
	}
	if err := a.recordBeforeApply(residueDeny, filepath.Join(tmp, "a-root")); err != nil {
		t.Fatalf("record run A: %v", err)
	}
	if err := b.recordBeforeApply(residueGrant, filepath.Join(tmp, "b-root")); err != nil {
		t.Fatalf("record run B: %v", err)
	}

	a.clear()
	if pathExists(a.marker) {
		t.Fatal("run A marker was not cleared")
	}
	if !pathExists(b.marker) {
		t.Fatal("run A cleanup removed run B marker")
	}
	sweepWindowsDenyResidue()
	if !pathExists(b.marker) {
		t.Fatal("sweep removed a live same-process marker")
	}
	b.clear()
	if pathExists(b.marker) {
		t.Fatal("run B marker was not cleared")
	}
}

func TestSweepableResidueRefusesSystemPaths(t *testing.T) {
	// The sweep must never act on a marker entry that names a system directory,
	// even though the current recorder can no longer write one: a marker left by
	// an older binary, or planted in the same-user-writable %TEMP%, would
	// otherwise drive icacls /remove:g of the broad built-in package SIDs against
	// System32 / Program Files and strip factory ACEs. User paths must stay
	// sweepable or crash residue would never be cleaned.
	userDir := t.TempDir()
	for _, e := range []residueEntry{
		{kind: residueDeny, path: filepath.Join(userDir, ".ssh")},
		{kind: residueGrant, path: userDir},
	} {
		if !sweepableResidue(e) {
			t.Fatalf("user path %q must be sweepable", e.path)
		}
	}

	sysExe := systemRootTool("icacls.exe")
	if !filepath.IsAbs(sysExe) {
		t.Skip("no absolute system tool to derive a system directory from")
	}
	sysDir := filepath.Dir(sysExe)
	for _, e := range []residueEntry{
		{kind: residueGrant, path: sysDir},
		{kind: residueDeny, path: sysDir},
		{kind: residueGrant, path: filepath.Join(sysDir, "sub")},
	} {
		if sweepableResidue(e) {
			t.Fatalf("system path %q must never be sweepable", e.path)
		}
	}
	if root := os.Getenv("ProgramFiles"); root != "" {
		if sweepableResidue(residueEntry{kind: residueGrant, path: root}) {
			t.Fatalf("Program Files root %q must never be sweepable", root)
		}
	}
}

func TestWindowsMutatedRootsForRunLocksNonSystemExeDirOnly(t *testing.T) {
	workspace := t.TempDir()
	// A non-system tool directory must join the lock set; a Windows system
	// directory must not, or commands sharing the system shell would needlessly
	// serialize. Membership is by path, not by a write probe, so this holds even
	// when the test process is elevated (as CI's runner is).
	toolDir := t.TempDir()
	toolExe := filepath.Join(toolDir, "mytool.exe")
	if err := os.WriteFile(toolExe, []byte("not really an exe"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := windowsMutatedRootsForRun(Spec{WritableRoots: []string{workspace}, Writable: true}, toolExe)
	if !containsWindowsPath(got, workspace) {
		t.Fatalf("mutated roots = %v, want workspace", got)
	}
	if !containsWindowsPath(got, toolDir) {
		t.Fatalf("mutated roots = %v, want tool dir %s", got, toolDir)
	}

	// A system executable's directory (System32) must be excluded regardless of
	// the process's integrity level. Resolve a real system tool to avoid
	// depending on PATH layout.
	sysExe := systemRootTool("icacls.exe")
	if !filepath.IsAbs(sysExe) {
		t.Skip("no absolute system tool to test the system-root exclusion")
	}
	sysRoots := windowsMutatedRootsForRun(Spec{WritableRoots: []string{workspace}, Writable: true}, sysExe)
	if containsWindowsPath(sysRoots, filepath.Dir(sysExe)) {
		t.Fatalf("system exe dir %s must not join the lock set: %v", filepath.Dir(sysExe), sysRoots)
	}
}

func TestWindowsMutableExecutableGrantRootsExcludesSystemDirs(t *testing.T) {
	// The grant loop (grantAppContainerExecutable) and the per-root lock both draw
	// their executable roots from windowsMutableExecutableGrantRoots, so it is the
	// single guard that keeps system directories from being snapshotted, granted,
	// or recorded as crash residue. A residue entry on System32 would let a later
	// sweep run icacls /remove:g for the broad built-in package SIDs and strip the
	// directory's factory ACEs, so a system tool dir must never appear here.
	toolDir := t.TempDir()
	toolExe := filepath.Join(toolDir, "mytool.exe")
	if err := os.WriteFile(toolExe, []byte("not really an exe"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := windowsMutableExecutableGrantRoots(toolExe); !containsWindowsPath(got, toolDir) {
		t.Fatalf("grant roots = %v, want non-system tool dir %s", got, toolDir)
	}

	sysExe := systemRootTool("icacls.exe")
	if !filepath.IsAbs(sysExe) {
		t.Skip("no absolute system tool to test the system-root exclusion")
	}
	sysDir := filepath.Dir(sysExe)
	got := windowsMutableExecutableGrantRoots(sysExe)
	if containsWindowsPath(got, sysDir) {
		t.Fatalf("system exe dir %s must never be a mutable grant root: %v", sysDir, got)
	}
	// The same executable must still resolve to a non-empty grant candidate before
	// the system filter, so the exclusion is what drops it — not a resolution miss.
	if len(windowsExecutableGrantRoots(sysExe)) == 0 {
		t.Fatalf("system exe %s should resolve to a grant candidate before filtering", sysExe)
	}
}

func TestIsWindowsSystemRoot(t *testing.T) {
	// System locations are excluded by path, independent of writability.
	sysExe := systemRootTool("icacls.exe")
	if filepath.IsAbs(sysExe) && !isWindowsSystemRoot(filepath.Dir(sysExe)) {
		t.Fatalf("%s should be classified as a system root", filepath.Dir(sysExe))
	}
	if root := os.Getenv("ProgramFiles"); root != "" {
		if !isWindowsSystemRoot(filepath.Join(root, "SomeApp")) {
			t.Fatalf("a path under %s should be a system root", root)
		}
	}
	// A user temp directory is never a system root.
	if isWindowsSystemRoot(t.TempDir()) {
		t.Fatal("a user temp dir must not be classified as a system root")
	}
}

func TestWindowsProcessAliveDetectsSelfAndDead(t *testing.T) {
	if !windowsProcessAlive(strconv.Itoa(os.Getpid())) {
		t.Fatal("current process should be reported alive")
	}
	// PID 0 and garbage never map to a live user process.
	for _, dead := range []string{"0", "not-a-pid", ""} {
		if windowsProcessAlive(dead) {
			t.Fatalf("%q should not be reported alive", dead)
		}
	}
}

func TestWindowsMutatedRootsIncludesForbidReadThatExists(t *testing.T) {
	workspace := t.TempDir()
	secret := filepath.Join(workspace, "secret")
	if err := os.Mkdir(secret, 0o755); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(workspace, "does-not-exist")
	got := windowsMutatedRoots(Spec{
		WritableRoots:   []string{workspace},
		ForbidReadRoots: []string{secret, missing},
		Writable:        true,
	})
	if !containsWindowsPath(got, workspace) || !containsWindowsPath(got, secret) {
		t.Fatalf("mutated roots = %v, want workspace and existing secret", got)
	}
	if containsWindowsPath(got, missing) {
		t.Fatalf("mutated roots must skip missing forbid_read paths: %v", got)
	}
}

func TestWindowsICACLSTimeoutRecursiveVsFlat(t *testing.T) {
	os.Unsetenv("WINDOWS_SANDBOX_ICACLS_TIMEOUT_MS")
	if got := icaclsTimeoutForArgs([]string{"/setintegritylevel", "L", "/T", "/C"}); got != defaultICACLSRecursiveTimeout {
		t.Fatalf("recursive timeout = %s, want %s", got, defaultICACLSRecursiveTimeout)
	}
	if got := icaclsTimeoutForArgs([]string{"/setintegritylevel", "M", "/C"}); got != defaultICACLSTimeout {
		t.Fatalf("flat timeout = %s, want %s", got, defaultICACLSTimeout)
	}
	t.Setenv("WINDOWS_SANDBOX_ICACLS_TIMEOUT_MS", "1234")
	if got := icaclsTimeoutForArgs([]string{"/T"}); got != 1234*time.Millisecond {
		t.Fatalf("env override = %s, want 1.234s", got)
	}
}

func TestSystemRootToolResolvesUnderSystem32(t *testing.T) {
	got := systemRootTool("icacls.exe")
	// On any real Windows host this resolves to an absolute System32 path; the
	// fallback (bare name) only happens if the file is genuinely missing.
	if got == "icacls.exe" {
		t.Skip("icacls.exe not found under System32 on this host")
	}
	if !filepath.IsAbs(got) || !strings.EqualFold(filepath.Base(got), "icacls.exe") {
		t.Fatalf("resolved tool = %q, want absolute System32 icacls.exe", got)
	}
	if !strings.Contains(strings.ToLower(got), `system32`) {
		t.Fatalf("resolved tool = %q, want a System32 path", got)
	}
}

// TestWindowsSandboxConcurrentWritesToSharedWorkspace exercises the concurrency
// fix end-to-end: several sandboxed writable commands run at once against the
// same non-empty workspace with nested directories. Before the root lock, their
// ACL/label mutations and snapshot restores would interleave and one command's
// cleanup could revoke another's write grant mid-run, surfacing as a spurious
// failure. With serialization every command must succeed and its file must be
// written. The empty-temp-dir CI coverage that shipped originally could not
// catch this.
func TestWindowsSandboxConcurrentWritesToSharedWorkspace(t *testing.T) {
	if !Available() {
		t.Skip("windows sandbox APIs unavailable")
	}
	sh := powershellArgvForTest(t, "")
	if sh == nil {
		t.Skip("PowerShell unavailable")
	}
	workspace := t.TempDir()
	// Pre-populate with nested content so the writable relabel actually walks a
	// subtree (the empty-dir case hid #1/#2/#3).
	nested := filepath.Join(workspace, "pkg", "sub")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"a.txt", filepath.Join("pkg", "b.txt"), filepath.Join("pkg", "sub", "c.txt")} {
		if err := os.WriteFile(filepath.Join(workspace, f), []byte("old"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	const n = 4
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			target := filepath.Join(workspace, "out"+strconv.Itoa(idx)+".txt")
			script := "$ErrorActionPreference='Stop'; Set-Content -LiteralPath " + psQuote(target) + " -Value ok"
			result, err := Run(
				Spec{WritableRoots: []string{workspace}, Network: true, Writable: true, TempPrefix: "windows-sandbox-test-"},
				append(sh, script),
				RunOptions{Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr},
			)
			if err != nil {
				errs[idx] = err
				return
			}
			if result.ExitCode != 0 {
				errs[idx] = errExitf(idx, result.ExitCode)
			}
		}(i)
	}
	wg.Wait()

	for i := 0; i < n; i++ {
		if errs[i] != nil {
			t.Fatalf("concurrent command %d failed: %v", i, errs[i])
		}
		target := filepath.Join(workspace, "out"+strconv.Itoa(i)+".txt")
		if got, err := os.ReadFile(target); err != nil || !strings.Contains(string(got), "ok") {
			t.Fatalf("concurrent command %d output missing: %q err=%v", i, got, err)
		}
	}
	// After all runs the workspace must carry no leftover Low integrity label or
	// sandbox deny ACE.
	assertNoWindowsSandboxACEForTest(t, workspace)
}

// TestWindowsSandboxConcurrentDistinctWorkspacesSharedToolDir is the P2
// regression: two runs in *different* workspaces (so their workspace locks do
// not collide) that invoke a tool from one shared user-writable directory. That
// shared exe directory is snapshot/grant/restored by each run, so without
// folding it into the lock the two runs would interleave their ACL snapshots and
// one could fail or leave a stale grant. Both must succeed and the shared tool
// directory must carry no leftover sandbox ACE.
func TestWindowsSandboxConcurrentDistinctWorkspacesSharedToolDir(t *testing.T) {
	if !Available() {
		t.Skip("windows sandbox APIs unavailable")
	}
	// Stage a copy of cmd.exe in a shared user directory as the tool. cmd.exe is a
	// single self-contained binary with no side-by-side runtime dependencies, so a
	// copy runs standalone — unlike pwsh.exe, which needs its hostfxr/DLLs next to
	// it. Each concurrent run resolves argv[0] into this shared directory, so the
	// runs contend on it even though their workspaces differ.
	cmdExe := systemRootTool("cmd.exe")
	if !filepath.IsAbs(cmdExe) {
		t.Skip("cmd.exe not found under System32")
	}
	toolDir := t.TempDir()
	sharedTool := filepath.Join(toolDir, "sandbox-shared-tool.exe")
	if err := copyFileForTest(t, cmdExe, sharedTool); err != nil {
		t.Skipf("cannot stage shared tool: %v", err)
	}

	const n = 4
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			workspace := t.TempDir() // distinct per run: workspace locks do not collide
			target := filepath.Join(workspace, "out.txt")
			// cmd.exe /c echo ok> "<target>". No spaces before '>' so the redirect
			// target is exactly the path.
			toolArgv := []string{sharedTool, "/c", "echo ok> " + target}
			result, err := Run(
				Spec{WritableRoots: []string{workspace}, Network: true, Writable: true, TempPrefix: "windows-sandbox-test-"},
				toolArgv,
				RunOptions{Stdin: os.Stdin, Stdout: os.Stdout, Stderr: os.Stderr},
			)
			if err != nil {
				errs[idx] = err
				return
			}
			if result.ExitCode != 0 {
				errs[idx] = errExitf(idx, result.ExitCode)
			}
		}(i)
	}
	wg.Wait()

	for i := 0; i < n; i++ {
		if errs[i] != nil {
			t.Fatalf("concurrent run %d sharing tool dir failed: %v", i, errs[i])
		}
	}
	// The shared tool directory must be left with no sandbox grant residue.
	assertNoWindowsSandboxACEForTest(t, toolDir)
}

func copyFileForTest(t *testing.T, src, dst string) error {
	t.Helper()
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o755)
}

func contains(haystack []string, needle string) bool {
	for _, h := range haystack {
		if h == needle {
			return true
		}
	}
	return false
}

func containsWindowsPath(haystack []string, needle string) bool {
	for _, h := range haystack {
		if sameWindowsPath(h, needle) {
			return true
		}
	}
	return false
}

func errExitf(idx, code int) error {
	return &exitError{idx: idx, code: code}
}

type exitError struct {
	idx  int
	code int
}

func (e *exitError) Error() string {
	return "command " + strconv.Itoa(e.idx) + " exit code " + strconv.Itoa(e.code)
}
