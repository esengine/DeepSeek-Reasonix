package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"reasonix/internal/sandbox"
	"reasonix/internal/sandboxauth"
)

func TestCapabilityGrantInvalidEntryIsolationAndCanonicalDedupe(t *testing.T) {
	root := t.TempDir()
	readDir := filepath.Join(root, "read")
	if err := os.Mkdir(readDir, 0o755); err != nil {
		t.Fatal(err)
	}
	executable := capabilityTestExecutable(t)
	body := fmt.Sprintf(`
[[sandbox.capability_grants]]
canonical_executable = %q
argv_prefix = ["echo", "invalid"]
network = "not-a-bool"

[[sandbox.capability_grants]]
canonical_executable = %q
argv_prefix = ["echo"]
network = true
[[sandbox.capability_grants.reads]]
identity = "workspace_relative"
path = "read"
kind = "directory"

[[sandbox.capability_grants]]
canonical_executable = %q
argv_prefix = ["echo"]
network = true
[[sandbox.capability_grants.reads]]
identity = "canonical_absolute"
path = %q
kind = "directory"
`, executable, executable, executable, readDir)
	if err := os.WriteFile(filepath.Join(root, "reasonix.toml"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadForRootReadOnly(root); err != nil {
		t.Fatalf("invalid grant disabled unrelated config loading: %v", err)
	}
	grants, diagnostics := LoadProjectCapabilityGrants(root)
	if len(grants) != 1 {
		t.Fatalf("grants=%+v, want one canonical grant", grants)
	}
	if len(diagnostics) != 1 || diagnostics[0].Entry != 0 || diagnostics[0].Code != "decode" {
		t.Fatalf("diagnostics=%+v", diagnostics)
	}
}

func TestPersistCapabilityGrantTargetedAppendIsIdempotentAndComplete(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "reasonix.toml")
	original := "# keep this comment\n[unknown]\nvalue = 42\n"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	grant := sandboxauth.Grant{
		Workspace: root, CanonicalExecutable: capabilityTestExecutable(t), ArgvPrefix: []string{"echo", "ok"},
		Capabilities: sandbox.CapabilitySet{Network: true, Devices: []sandbox.CapabilityDevice{{Path: "/dev/null", Canonical: "/dev/null", Kind: sandbox.CapabilityCharacterDevice, Major: 1, Minor: 3}}},
		Background:   true, PreserveBackgroundProcesses: true,
	}
	if err := PersistProjectCapabilityGrant(root, grant); err != nil {
		t.Fatal(err)
	}
	first, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := PersistProjectCapabilityGrant(root, grant); err != nil {
		t.Fatal(err)
	}
	second, _ := os.ReadFile(path)
	if string(first) != string(second) {
		t.Fatal("exact duplicate persistence rewrote the file")
	}
	text := string(second)
	for _, want := range []string{"# keep this comment", "[unknown]", "[[sandbox.capability_grants]]", "[[sandbox.capability_grants.devices]]", "major = 1", "preserve_background_processes = true"} {
		if !strings.Contains(text, want) {
			t.Fatalf("persisted file missing %q:\n%s", want, text)
		}
	}
	cfg, err := LoadForEditReadOnlyStrict(path)
	if err != nil {
		t.Fatal(err)
	}
	cfg.Sandbox.Network = false
	if err := cfg.SaveTo(path); err != nil {
		t.Fatalf("ordinary project config edit after grant: %v", err)
	}
	if grants, diagnostics := LoadProjectCapabilityGrants(root); len(grants) != 1 || len(diagnostics) != 0 {
		t.Fatalf("ordinary config edit lost grant: grants=%+v diagnostics=%+v", grants, diagnostics)
	}
}

func TestCapabilityGrantPersistenceDoesNotMergeBroaderAndNarrower(t *testing.T) {
	root := t.TempDir()
	parent := filepath.Join(root, "data")
	child := filepath.Join(parent, "nested")
	if err := os.MkdirAll(child, 0o755); err != nil {
		t.Fatal(err)
	}
	base := sandboxauth.Grant{Workspace: root, CanonicalExecutable: capabilityTestExecutable(t), ArgvPrefix: []string{"echo"}}
	broad := base
	broad.Capabilities.Reads = []sandbox.CapabilityPath{{Identity: sandbox.WorkspaceRelative, Path: "data", Canonical: parent, Kind: sandbox.CapabilityDirectory}}
	narrow := base
	narrow.Capabilities.Reads = []sandbox.CapabilityPath{{Identity: sandbox.WorkspaceRelative, Path: "data/nested", Canonical: child, Kind: sandbox.CapabilityDirectory}}
	if err := PersistProjectCapabilityGrant(root, broad); err != nil {
		t.Fatal(err)
	}
	if err := PersistProjectCapabilityGrant(root, narrow); err != nil {
		t.Fatal(err)
	}
	grants, diagnostics := LoadProjectCapabilityGrants(root)
	if len(grants) != 2 || len(diagnostics) != 0 {
		t.Fatalf("grants=%+v diagnostics=%+v", grants, diagnostics)
	}
}

func TestCapabilityGrantPersistenceErrorsKeepContextAndCause(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "reasonix.toml"), 0o755); err != nil {
		t.Fatal(err)
	}
	grant := sandboxauth.Grant{Workspace: root, CanonicalExecutable: capabilityTestExecutable(t), ArgvPrefix: []string{"echo"}, Capabilities: sandbox.CapabilitySet{Network: true}}
	err := PersistProjectCapabilityGrant(root, grant)
	if err == nil || !strings.Contains(err.Error(), "persist capability grant: strict fresh read") {
		t.Fatalf("error=%v", err)
	}
	var pathErr *os.PathError
	if !errors.As(err, &pathErr) {
		t.Fatalf("wrapped cause was not preserved: %T %v", err, err)
	}
}

func TestConcurrentPermissionAndCapabilityTransactionsPreserveBoth(t *testing.T) {
	root := t.TempDir()
	t.Setenv("REASONIX_HOME", filepath.Join(t.TempDir(), "reasonix-home"))
	path := filepath.Join(root, "reasonix.toml")
	if err := os.WriteFile(path, []byte("# shared\n[permissions]\nallow = []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	grant := sandboxauth.Grant{Workspace: root, CanonicalExecutable: capabilityTestExecutable(t), ArgvPrefix: []string{"echo"}, Capabilities: sandbox.CapabilitySet{Network: true}}
	start := make(chan struct{})
	errCh := make(chan error, 2)
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		<-start
		errCh <- PersistProjectCapabilityGrant(root, grant)
	}()
	go func() {
		defer wg.Done()
		<-start
		unlock, err := LockConfigFileEdits(path)
		if err != nil {
			errCh <- err
			return
		}
		defer unlock()
		cfg, err := LoadForEditReadOnlyStrict(path)
		if err == nil {
			cfg.Permissions.Allow = []string{"Bash(go test:*)"}
			err = WritePermissionsAllow(path, cfg.Permissions.Allow)
		}
		errCh <- err
	}()
	close(start)
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatal(err)
		}
	}
	grants, diagnostics := LoadProjectCapabilityGrants(root)
	if len(grants) != 1 || len(diagnostics) != 0 {
		t.Fatalf("grants=%+v diagnostics=%+v", grants, diagnostics)
	}
	cfg, err := LoadForEditReadOnlyStrict(path)
	if err != nil || len(cfg.Permissions.Allow) != 1 {
		t.Fatalf("permission transaction lost: cfg=%+v err=%v", cfg, err)
	}
}

func capabilityTestExecutable(t *testing.T) string {
	t.Helper()
	path, err := filepath.EvalSymlinks("/bin/echo")
	if err != nil {
		path = "/bin/echo"
	}
	return path
}

var _ sandboxauth.GrantSource = CapabilityGrantStore{}
var _ sandboxauth.Persister = CapabilityGrantStore{}
