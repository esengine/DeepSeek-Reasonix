package plugin

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"reasonix/internal/safety/sandbox"
)

func TestCompositionProcessStateIsPrivateAndConfinedToOwnedSubdirectory(t *testing.T) {
	base := t.TempDir()
	for _, name := range []string{"one", "two"} {
		root := filepath.Join(base, "sessions", name)
		_, env, err := prepareMCPPrivateStateForOS(Spec{StateDir: base, ProcessStateDir: root}, sandbox.Spec{}, nil, "linux")
		if err != nil {
			t.Fatal(err)
		}
		if got, _ := envValue(env, "XDG_STATE_HOME"); got != filepath.Join(root, "state") {
			t.Fatalf("state=%s", got)
		}
	}
	for _, path := range []string{base, filepath.Join(base, "sessions"), filepath.Join(base, "sessions", "..", "outside"), filepath.Join(base, "sessions", "nested", "child")} {
		if _, _, err := prepareMCPPrivateStateForOS(Spec{StateDir: base, ProcessStateDir: path}, sandbox.Spec{}, nil, "linux"); err == nil {
			t.Fatalf("unsafe state accepted: %s", path)
		}
	}
	keep := filepath.Join(base, "credential-reference")
	if err := os.WriteFile(keep, []byte("shared"), 0o600); err != nil {
		t.Fatal(err)
	}
	transport := &stdioTransport{privateState: filepath.Join(base, "sessions", "one")}
	transport.close()
	if _, err := os.Stat(transport.privateState); !os.IsNotExist(err) {
		t.Fatalf("private state remains: %v", err)
	}
	if _, err := os.Stat(keep); err != nil {
		t.Fatal("credential store removed")
	}
}

func TestCompositionProcessStateRejectsSymlinkOwner(t *testing.T) {
	base, outside := t.TempDir(), t.TempDir()
	if err := os.Symlink(outside, filepath.Join(base, "sessions")); err != nil {
		t.Fatal(err)
	}
	if _, err := CompositionProcessRoot(Spec{StateDir: base, ProcessStateDir: filepath.Join(base, "sessions", "session")}); err == nil {
		t.Fatal("symlink process state accepted")
	}
	if entries, err := os.ReadDir(outside); err != nil || len(entries) != 0 {
		t.Fatalf("outside directory changed: %v %v", entries, err)
	}
}

func TestCompositionMCPReconnectKeepsSessionOwnedPackageResources(t *testing.T) {
	redirectCache(t)
	spec := helperSpec()
	spec.StateDir = t.TempDir()
	spec.ProcessStateDir = filepath.Join(spec.StateDir, "sessions", "fixture")
	spec.Package, spec.Authorized, spec.ProcessMode = "fixture", true, MCPProcessHost
	entry := filepath.Join(spec.ProcessStateDir, "package", "entry")
	if err := os.MkdirAll(filepath.Dir(entry), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(entry, []byte("retained code"), 0o600); err != nil {
		t.Fatal(err)
	}
	host := NewHost()
	defer host.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	for range 2 {
		if _, err := host.Add(ctx, spec); err != nil {
			t.Fatal(err)
		}
		host.Remove(spec.Name)
		if data, err := os.ReadFile(entry); err != nil || string(data) != "retained code" {
			t.Fatalf("transport retired its session owner's package: %s %v", data, err)
		}
		if _, err := os.Stat(compositionTransportState(spec)); !os.IsNotExist(err) {
			t.Fatalf("connection state was not retired: %v", err)
		}
	}
}
