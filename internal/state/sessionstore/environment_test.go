package sessionstore

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestEnvironmentBindingRequiresLiveAuthorityAndIsImmutable(t *testing.T) {
	path, _ := leaseTestPath(t)
	lease, err := TryAcquireSessionLease(path)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Release()
	auth, err := lease.IssueWriteAuthority(NextSessionWriteGeneration())
	if err != nil {
		t.Fatal(err)
	}
	r := ExecutionEnvironmentBinding{Version: 1, EnvironmentID: "renv_test", Generation: "generation", SourceWorkspace: t.TempDir(), WorkspaceRoot: t.TempDir(), WorkingCopyRoot: t.TempDir()}
	if err := BindExecutionEnvironment(path, r, nil); !errors.Is(err, ErrSessionWriteAuthorityMissing) {
		t.Fatalf("missing authority: %v", err)
	}
	if err := BindExecutionEnvironment(path, r, auth); err != nil {
		t.Fatal(err)
	}
	if err := BindExecutionEnvironment(path, r, auth); err != nil {
		t.Fatal("idempotent bind:", err)
	}
	r.EnvironmentID = "renv_other"
	if err := BindExecutionEnvironment(path, r, auth); !errors.Is(err, ErrExecutionEnvironmentMismatch) {
		t.Fatalf("binding replaced: %v", err)
	}
	lease.Release()
	if err := BindExecutionEnvironment(path, r, auth); !errors.Is(err, ErrSessionWriteAuthorityStale) {
		t.Fatalf("stale authority: %v", err)
	}
}

func TestEnvironmentBindingUnknownVersionAndMissingRootNeverFallBack(t *testing.T) {
	path, _ := leaseTestPath(t)
	write := func(text string) {
		t.Helper()
		if err := os.WriteFile(ExecutionEnvironmentPath(path), []byte(text), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(`{"version":999,"future":"retained"}`)
	if _, ok, err := LoadExecutionEnvironment(path); !ok || !errors.Is(err, ErrExecutionEnvironmentProtocol) {
		t.Fatalf("unknown = %v %v", ok, err)
	}
	if err := CheckExecutionEnvironment(path, t.TempDir()); !errors.Is(err, ErrExecutionEnvironmentProtocol) {
		t.Fatalf("unknown allowed: %v", err)
	}
	got, _ := os.ReadFile(ExecutionEnvironmentPath(path))
	if string(got) != `{"version":999,"future":"retained"}` {
		t.Fatal("unknown fields rewritten")
	}
	if err := os.Remove(ExecutionEnvironmentPath(path)); err != nil {
		t.Fatal(err)
	}
	lease, err := TryAcquireSessionLease(path)
	if err != nil {
		t.Fatal(err)
	}
	defer lease.Release()
	auth, err := lease.IssueWriteAuthority(NextSessionWriteGeneration())
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	r := ExecutionEnvironmentBinding{Version: 1, EnvironmentID: "renv_test", Generation: "generation", SourceWorkspace: t.TempDir(), WorkspaceRoot: root, WorkingCopyRoot: root}
	if err := BindExecutionEnvironment(path, r, auth); err != nil {
		t.Fatal(err)
	}
	if err := CheckExecutionEnvironment(path, r.SourceWorkspace); !errors.Is(err, ErrExecutionEnvironmentMismatch) {
		t.Fatalf("source allowed: %v", err)
	}
	if err := CheckExecutionEnvironment(path, filepath.Join(root, "..")); !errors.Is(err, ErrExecutionEnvironmentMismatch) {
		t.Fatalf("different scope allowed: %v", err)
	}
	if err := CheckExecutionEnvironment(path, root); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}
	if err := CheckExecutionEnvironment(path, root); err == nil {
		t.Fatal("missing environment allowed")
	}
}
