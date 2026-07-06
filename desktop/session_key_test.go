package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"reasonix/internal/agent"
)

// The lease registry folds session paths through agent.CanonicalSessionPath
// (lowercased on Windows). Every desktop "same session" comparison must fold
// the same way, or a lease the tab itself holds looks foreign and every
// model/effort/token switch self-locks with the "already open in another
// window" error (#5999, #6006, #5996).

func TestSessionRuntimeKeyMatchesLeaseKey(t *testing.T) {
	dir := t.TempDir()
	// Mixed-case segment on every platform; on Windows the lease key folds it.
	path := filepath.Join(dir, "Sessions-Dir", "20260705-Test.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	lease, err := agent.TryAcquireSessionLease(path)
	if err != nil {
		t.Fatalf("acquire lease: %v", err)
	}
	defer lease.Release()

	if got, want := sessionRuntimeKey(lease.Path()), sessionRuntimeKey(path); got != want {
		t.Fatalf("lease path key %q != session path key %q; lease reuse checks will self-lock", got, want)
	}
}

func TestSessionRuntimeKeyIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "Case-Mix", "20260705-Test.jsonl")
	key := sessionRuntimeKey(path)
	if key == "" {
		t.Fatal("empty key for non-empty path")
	}
	if again := sessionRuntimeKey(key); again != key {
		t.Fatalf("sessionRuntimeKey not idempotent: %q -> %q", key, again)
	}
}

func TestSessionRuntimeKeyFoldsCaseOnWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("case-insensitive path identity is Windows-only")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "Sessions", "20260705-Test.jsonl")
	upper := strings.ToUpper(path)
	if sessionRuntimeKey(path) != sessionRuntimeKey(upper) {
		t.Fatalf("case variants of one file produced distinct keys: %q vs %q",
			sessionRuntimeKey(path), sessionRuntimeKey(upper))
	}
}

func TestCanonicalTabSessionPathNormalizesOutsideSessionDir(t *testing.T) {
	// Project-scope sessions live outside config.SessionDir(); the fallback
	// must still clean the shape so one file cannot split into two keys.
	dir := t.TempDir()
	base := filepath.Join(dir, "projects", "p1", "sessions", "20260705-a.jsonl")
	messy := filepath.Join(dir, "projects", "p1", ".", "sessions") + string(filepath.Separator) + "20260705-a.jsonl"
	if sessionRuntimeKey(base) != sessionRuntimeKey(messy) {
		t.Fatalf("path shape variants split keys: %q vs %q",
			sessionRuntimeKey(base), sessionRuntimeKey(messy))
	}
}

func TestEnsureSessionLeaseReusesHeldLease(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "Sessions", "20260705-Reuse.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	tab := &WorkspaceTab{ID: "tab_reuse"}
	if err := tab.ensureSessionLease(path); err != nil {
		t.Fatalf("first ensure: %v", err)
	}
	defer tab.releaseSessionLease()

	acquires := 0
	sessionLeaseAcquireHookForTest = func() { acquires++ }
	defer func() { sessionLeaseAcquireHookForTest = nil }()

	// Same path again — and the lease's own (canonical) form: both must hit
	// the fast path instead of re-acquiring against our own registry entry.
	if err := tab.ensureSessionLease(path); err != nil {
		t.Fatalf("re-ensure same path: %v", err)
	}
	if err := tab.ensureSessionLease(tab.sessionLeaseRuntimeKey()); err != nil {
		t.Fatalf("re-ensure canonical form: %v", err)
	}
	if acquires != 0 {
		t.Fatalf("expected fast-path reuse, got %d new acquires", acquires)
	}
}

func TestCanReclaimAllowsMissingLeaseInfo(t *testing.T) {
	// lease.json deleted or torn: Info is nil but nothing actually holds the
	// session. The reclaim attempt must be allowed — the OS lock arbitrates —
	// instead of wedging the session as busy on missing metadata (#5999's
	// fresh-install variant).
	app := NewApp()
	tab := &WorkspaceTab{ID: "tab_nil_info"}
	app.tabs = map[string]*WorkspaceTab{tab.ID: tab}
	path := filepath.Join(t.TempDir(), "Sessions", "20260705-a.jsonl")

	errNil := &agent.SessionLeaseError{Path: path}
	if !app.canReclaimCurrentProcessSessionLease(tab, path, errNil) {
		t.Fatal("nil-Info lease error must allow a reclaim attempt")
	}

	foreign := &agent.SessionLeaseError{Path: path, Info: &agent.SessionLeaseInfo{
		SessionPath: path, WriterID: "other-host-1-deadbeef", PID: os.Getpid() + 1,
	}}
	if app.canReclaimCurrentProcessSessionLease(tab, path, foreign) {
		t.Fatal("foreign-holder lease error must not allow reclaim")
	}
}

func TestSessionLeaseBusyErrorOmitsSettingClause(t *testing.T) {
	generic := (&sessionLeaseBusyError{}).Error()
	if strings.Contains(generic, "before changing") {
		t.Fatalf("empty-setting busy error still names a setting: %q", generic)
	}
	withSetting := (&sessionLeaseBusyError{setting: "model"}).Error()
	if !strings.Contains(withSetting, "before changing model") {
		t.Fatalf("setting busy error lost its clause: %q", withSetting)
	}
}
