//go:build linux

package sandbox

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLinuxEffectiveReadDeltaModelsTmpfsMaskAndReboundWriteRoot(t *testing.T) {
	workspace, err := os.MkdirTemp("/tmp", "reasonix-capability-workspace-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(workspace) })
	external, err := os.MkdirTemp("/tmp", "reasonix-capability-external-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(external) })

	base := Spec{Mode: "enforce", WriteRoots: []string{workspace}, MinimalWrites: true}
	visible := CapabilityPath{Canonical: workspace, Kind: CapabilityDirectory}
	hidden := CapabilityPath{Canonical: external, Kind: CapabilityDirectory}
	delta := effectiveCapabilityDelta(base, CapabilitySet{Reads: []CapabilityPath{visible, hidden}})
	if len(delta.Reads) != 1 || delta.Reads[0].Canonical != external {
		t.Fatalf("read delta = %#v, want only masked host path %q", delta.Reads, external)
	}
}

func TestLinuxEffectiveReadDeltaModelsAllReplacementMounts(t *testing.T) {
	for _, target := range []string{"/tmp", "/dev", "/proc", "/"} {
		t.Run(target, func(t *testing.T) {
			requested := CapabilityPath{Canonical: target, Kind: CapabilityDirectory}
			delta := effectiveCapabilityDelta(Spec{Mode: "enforce", MinimalWrites: true}, CapabilitySet{Reads: []CapabilityPath{requested}})
			if len(delta.Reads) != 1 || delta.Reads[0].Canonical != target {
				t.Fatalf("read delta = %#v, want replacement-intersecting scope %q retained", delta.Reads, target)
			}
		})
	}
}

func TestEvaluateCapabilitySoftDeniesSpecialFiles(t *testing.T) {
	raw := capabilityJSON(t, map[string]any{
		"read_paths": []any{map[string]string{"identity": string(CanonicalAbsolute), "path": "/dev/null"}},
	})
	review := EvaluateCapability(context.Background(), CapabilityInput{
		Base:      Spec{Mode: "enforce"},
		Workspace: t.TempDir(),
		Raw:       raw,
	}).Review()
	if review.State != CapabilitySoftDenied || !strings.Contains(review.Diagnostic, "regular file or directory") {
		t.Fatalf("review = %#v, want diagnosed whole-bundle soft denial", review)
	}
	if !capabilitySetEmpty(review.EffectiveDelta) {
		t.Fatalf("special-file request produced delta: %#v", review.EffectiveDelta)
	}
}

func TestLinuxDevPathScopesAreUnsupported(t *testing.T) {
	dev, ok := existingCapabilityRoot("/dev")
	if !ok {
		t.Skip("/dev is unavailable")
	}
	for name, test := range map[string]struct {
		path CapabilityPath
		want bool
	}{
		"dev root":       {path: dev, want: true},
		"dev descendant": {path: CapabilityPath{Canonical: filepath.Join(dev.Canonical, "dri"), Kind: CapabilityDirectory}, want: true},
		"ancestor root":  {path: CapabilityPath{Canonical: "/", Kind: CapabilityDirectory}, want: true},
		"tmp":            {path: CapabilityPath{Canonical: "/tmp", Kind: CapabilityDirectory}, want: false},
	} {
		t.Run(name, func(t *testing.T) {
			got := capabilitySetContainsDevScope(CapabilitySet{Reads: []CapabilityPath{test.path}})
			if got != test.want {
				t.Fatalf("capabilitySetContainsDevScope(%#v) = %v, want %v", test.path, got, test.want)
			}
		})
	}

	if !Available() {
		t.Skip("Bubblewrap unavailable")
	}
	raw := capabilityJSON(t, map[string]any{
		"network":    true,
		"read_paths": []any{map[string]string{"identity": string(CanonicalAbsolute), "path": dev.Canonical}},
	})
	review := EvaluateCapability(context.Background(), CapabilityInput{
		Base:      Spec{Mode: "enforce", MinimalWrites: true},
		Workspace: t.TempDir(),
		Raw:       raw,
	}).Review()
	if review.State != CapabilitySoftDenied || !strings.Contains(review.Diagnostic, "ordinary /dev path relaxation is unsupported") {
		t.Fatalf("review = %#v, want diagnosed /dev soft denial", review)
	}
	if review.Risk.Level != CapabilityRiskCritical {
		t.Fatalf("risk = %#v, want critical /dev scope", review.Risk)
	}
	if !capabilitySetEmpty(review.EffectiveDelta) {
		t.Fatalf("mixed /dev bundle produced partial delta: %#v", review.EffectiveDelta)
	}
}

func TestLinuxCapabilityLaunchUsesDescriptorBoundExactRead(t *testing.T) {
	if !Available() {
		t.Skip("Bubblewrap unavailable")
	}
	workspace := t.TempDir()
	secretDir := t.TempDir()
	allowed := filepath.Join(secretDir, "allowed.txt")
	sibling := filepath.Join(secretDir, "sibling.txt")
	if err := os.WriteFile(allowed, []byte("allowed"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sibling, []byte("sibling"), 0o644); err != nil {
		t.Fatal(err)
	}
	raw := capabilityJSON(t, map[string]any{
		"read_paths": []any{map[string]string{"identity": string(CanonicalAbsolute), "path": allowed}},
	})
	assessment := EvaluateCapability(context.Background(), CapabilityInput{
		Base: Spec{
			Mode:            "enforce",
			WriteRoots:      []string{workspace},
			ForbidReadRoots: []string{secretDir},
			Network:         true,
			MinimalWrites:   true,
		},
		Workspace: workspace,
		Raw:       raw,
	})
	if review := assessment.Review(); review.State != CapabilityReady {
		t.Skipf("descriptor-bound capability unavailable: state=%v diagnostic=%q", review.State, review.Diagnostic)
	}
	sh := ResolveShell("bash", "", nil)
	command := "test \"$(cat " + shellQuote(allowed) + ")\" = allowed" +
		" && ! ( printf changed > " + shellQuote(allowed) + " ) 2>/dev/null" +
		" && ! test -s " + shellQuote(sibling)
	launch := PrepareCapabilityCommand(context.Background(), assessment, AuthorizedDelta, sh, command)
	defer launch.Close()
	if !launch.UsesDelta || !containsArg(launch.Argv, "--ro-bind-fd") {
		t.Fatalf("launch = %#v, want descriptor-bound delta", launch)
	}
	cmd := exec.Command(launch.Argv[0], launch.Argv[1:]...)
	cmd.ExtraFiles = launch.ExtraFiles
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("capability command failed: %v\n%s\nargv=%v", err, out, launch.Argv)
	}
	if got, err := os.ReadFile(allowed); err != nil || string(got) != "allowed" {
		t.Fatalf("read-only file = %q, err=%v; descriptor mount accepted a write", got, err)
	}
}

func TestLinuxCapabilityLaunchKeepsExactWriteIndependent(t *testing.T) {
	if !Available() {
		t.Skip("Bubblewrap unavailable")
	}
	workspace := t.TempDir()
	external := t.TempDir()
	allowed := filepath.Join(external, "allowed.txt")
	sibling := filepath.Join(external, "sibling.txt")
	if err := os.WriteFile(allowed, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sibling, []byte("sibling"), 0o644); err != nil {
		t.Fatal(err)
	}
	assessment := EvaluateCapability(context.Background(), CapabilityInput{
		Base:      Spec{Mode: "enforce", WriteRoots: []string{workspace}, Network: true, MinimalWrites: true},
		Workspace: workspace,
		Raw: capabilityJSON(t, map[string]any{
			"write_paths": []any{map[string]string{"identity": string(CanonicalAbsolute), "path": allowed}},
		}),
	})
	if review := assessment.Review(); review.State != CapabilityReady {
		t.Skipf("descriptor-bound capability unavailable: state=%v diagnostic=%q", review.State, review.Diagnostic)
	}
	// Bubblewrap may let the command create an ephemeral sibling inside its
	// private /tmp. The host sibling must remain unchanged; only the FD-bound
	// exact file may receive host writes.
	command := "printf changed > " + shellQuote(allowed) +
		"; printf ephemeral > " + shellQuote(sibling)
	launch := PrepareCapabilityCommand(context.Background(), assessment, AuthorizedDelta, ResolveShell("bash", "", nil), command)
	defer launch.Close()
	cmd := exec.Command(launch.Argv[0], launch.Argv[1:]...)
	cmd.ExtraFiles = launch.ExtraFiles
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("exact write command failed: %v\n%s", err, out)
	}
	if got, err := os.ReadFile(allowed); err != nil || string(got) != "changed" {
		t.Fatalf("allowed file = %q, err=%v", got, err)
	}
	if got, err := os.ReadFile(sibling); err != nil || string(got) != "sibling" {
		t.Fatalf("sibling file = %q, err=%v", got, err)
	}
}

func TestLinuxCapabilityLaunchRequiresAndAppliesExplicitReadWithForbiddenWrite(t *testing.T) {
	if !Available() {
		t.Skip("Bubblewrap unavailable")
	}
	workspace := t.TempDir()
	secretDir := t.TempDir()
	target := filepath.Join(secretDir, "state.txt")
	if err := os.WriteFile(target, []byte("before"), 0o644); err != nil {
		t.Fatal(err)
	}
	pathRequest := map[string]string{"identity": string(CanonicalAbsolute), "path": target}
	assessment := EvaluateCapability(context.Background(), CapabilityInput{
		Base: Spec{
			Mode:            "enforce",
			WriteRoots:      []string{workspace},
			ForbidReadRoots: []string{secretDir},
			Network:         true,
			MinimalWrites:   true,
		},
		Workspace: workspace,
		Raw: capabilityJSON(t, map[string]any{
			"read_paths":  []any{pathRequest},
			"write_paths": []any{pathRequest},
		}),
	})
	if review := assessment.Review(); review.State != CapabilityReady || len(review.EffectiveDelta.Reads) != 1 || len(review.EffectiveDelta.Writes) != 1 {
		t.Fatalf("read/write assessment = %#v", review)
	}
	command := "test \"$(cat " + shellQuote(target) + ")\" = before && printf after > " + shellQuote(target)
	launch := PrepareCapabilityCommand(context.Background(), assessment, AuthorizedDelta, ResolveShell("bash", "", nil), command)
	defer launch.Close()
	cmd := exec.Command(launch.Argv[0], launch.Argv[1:]...)
	cmd.ExtraFiles = launch.ExtraFiles
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("explicit read/write command failed: %v\n%s", err, out)
	}
	if got, err := os.ReadFile(target); err != nil || string(got) != "after" {
		t.Fatalf("target = %q, err=%v", got, err)
	}
}

func TestLinuxCapabilityLaunchAtomicallyFallsBackAfterIdentityReplacement(t *testing.T) {
	if !Available() {
		t.Skip("Bubblewrap unavailable")
	}
	workspace := t.TempDir()
	target := filepath.Join(workspace, "target")
	if err := os.WriteFile(target, []byte("file"), 0o644); err != nil {
		t.Fatal(err)
	}
	raw := capabilityJSON(t, map[string]any{
		"write_paths": []any{map[string]string{"identity": string(WorkspaceRelative), "path": "target"}},
	})
	assessment := EvaluateCapability(context.Background(), CapabilityInput{
		Base:      Spec{Mode: "enforce", Network: true, MinimalWrites: true},
		Workspace: workspace,
		Raw:       raw,
	})
	if review := assessment.Review(); review.State != CapabilityReady {
		t.Skipf("descriptor-bound capability unavailable: state=%v diagnostic=%q", review.State, review.Diagnostic)
	}
	if err := os.Remove(target); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	launch := PrepareCapabilityCommand(context.Background(), assessment, AuthorizedDelta, ResolveShell("bash", "", nil), "true")
	defer launch.Close()
	if launch.UsesDelta || launch.Diagnostic == "" {
		t.Fatalf("launch = %#v, want diagnosed atomic base fallback", launch)
	}
	if containsArg(launch.Argv, "--bind-fd") || containsArg(launch.Argv, "--ro-bind-fd") {
		t.Fatalf("fallback retained partial capability args: %v", launch.Argv)
	}
}

func TestLinuxCapabilityLaunchAtomicallyFallsBackAfterSymlinkReplacement(t *testing.T) {
	if !Available() {
		t.Skip("Bubblewrap unavailable")
	}
	workspace := t.TempDir()
	target := filepath.Join(workspace, "target")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatal(err)
	}
	assessment := EvaluateCapability(context.Background(), CapabilityInput{
		Base:      Spec{Mode: "enforce", Network: true, MinimalWrites: true},
		Workspace: workspace,
		Raw: capabilityJSON(t, map[string]any{
			"write_paths": []any{map[string]string{"identity": string(WorkspaceRelative), "path": "target"}},
		}),
	})
	if review := assessment.Review(); review.State != CapabilityReady {
		t.Skipf("descriptor-bound capability unavailable: state=%v diagnostic=%q", review.State, review.Diagnostic)
	}
	if err := os.Remove(target); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), target); err != nil {
		t.Fatal(err)
	}
	launch := PrepareCapabilityCommand(context.Background(), assessment, AuthorizedDelta, ResolveShell("bash", "", nil), "true")
	defer launch.Close()
	if launch.UsesDelta || launch.Diagnostic == "" || containsArg(launch.Argv, "--bind-fd") {
		t.Fatalf("symlink replacement launch = %#v, want atomic base fallback", launch)
	}
}

func TestLinuxCapabilityLaunchEnablesNetworkOnlyBundle(t *testing.T) {
	if !Available() {
		t.Skip("Bubblewrap unavailable")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("host sandbox does not permit loopback sockets: %v", err)
	}
	defer listener.Close()
	port := listener.Addr().(*net.TCPAddr).Port
	received := make(chan string, 1)
	acceptErr := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			acceptErr <- err
			return
		}
		defer conn.Close()
		body, err := io.ReadAll(conn)
		if err != nil {
			acceptErr <- err
			return
		}
		received <- string(body)
	}()

	workspace := t.TempDir()
	assessment := EvaluateCapability(context.Background(), CapabilityInput{
		Base:      Spec{Mode: "enforce", WriteRoots: []string{workspace}, MinimalWrites: true},
		Workspace: workspace,
		Raw:       capabilityJSON(t, map[string]any{"network": true}),
	})
	if review := assessment.Review(); review.State != CapabilityReady {
		t.Skipf("network capability unavailable: state=%v diagnostic=%q", review.State, review.Diagnostic)
	}
	sh := ResolveShell("bash", "", nil)
	command := fmt.Sprintf("printf network-ok > /dev/tcp/127.0.0.1/%d", port)
	launch := PrepareCapabilityCommand(context.Background(), assessment, AuthorizedDelta, sh, command)
	defer launch.Close()
	if !launch.UsesDelta || containsArg(launch.Argv, "--unshare-net") {
		t.Fatalf("network launch = %#v", launch)
	}
	cmd := exec.Command(launch.Argv[0], launch.Argv[1:]...)
	cmd.ExtraFiles = launch.ExtraFiles
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("network capability command failed: %v\n%s", err, out)
	}
	select {
	case err := <-acceptErr:
		t.Fatal(err)
	case got := <-received:
		if got != "network-ok" {
			t.Fatalf("received %q", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for network capability connection")
	}
}

func containsArg(args []string, want string) bool {
	for _, arg := range args {
		if arg == want {
			return true
		}
	}
	return false
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
