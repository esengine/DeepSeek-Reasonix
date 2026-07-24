//go:build linux

package sandbox

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestEvaluateDeviceCapabilityStrictlyNormalizesExactCharacterDevice(t *testing.T) {
	device, err := InspectCapabilityDevice("/dev/null")
	if err != nil {
		t.Skipf("/dev/null is unavailable: %v", err)
	}
	review := EvaluateCapability(context.Background(), CapabilityInput{
		Base:      Spec{Mode: "off"},
		Workspace: t.TempDir(),
		Raw: capabilityJSON(t, map[string]any{
			"devices": []any{map[string]string{"path": "/dev/null"}},
		}),
	}).Review()
	if review.State != CapabilityNoEffectiveDelta || len(review.Request.Devices) != 1 {
		t.Fatalf("review = %#v", review)
	}
	got := review.Request.Devices[0]
	if got != device || got.Kind != CapabilityCharacterDevice {
		t.Fatalf("device = %#v, want %#v", got, device)
	}
	if review.Risk.Level != CapabilityRiskCritical || len(review.Risk.Findings) != 1 || review.Risk.Findings[0].Code != "device_access" {
		t.Fatalf("risk = %#v, want critical device_access", review.Risk)
	}
	if !review.Authority.Requested || review.Authority.Supported || review.Authority.Prepared || review.Authority.Applied != CapabilityNotApplied {
		t.Fatalf("authority = %#v", review.Authority)
	}
	diagnostic := CapabilityFallbackDiagnostic(review, BaseOnly)
	for _, want := range []string{"requested=true", "supported=false", "prepared=false", "applied=false"} {
		if !strings.Contains(diagnostic, want) {
			t.Fatalf("diagnostic %q missing %q", diagnostic, want)
		}
	}
}

func TestEvaluateDeviceCapabilityRejectsMalformedAndNonDevices(t *testing.T) {
	workspace := t.TempDir()
	regular := filepath.Join(workspace, "regular")
	if err := os.WriteFile(regular, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	directory := filepath.Join(workspace, "directory")
	if err := os.Mkdir(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	fifo := filepath.Join(workspace, "fifo")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	socket := filepath.Join(workspace, "socket")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	symlink := filepath.Join(workspace, "null-link")
	if err := os.Symlink("/dev/null", symlink); err != nil {
		t.Fatal(err)
	}

	tests := map[string]json.RawMessage{
		"null devices":     json.RawMessage(`{"devices":null}`),
		"relative":         capabilityJSON(t, map[string]any{"devices": []any{map[string]string{"path": "dev/null"}}}),
		"unclean":          capabilityJSON(t, map[string]any{"devices": []any{map[string]string{"path": "/dev/../dev/null"}}}),
		"missing":          capabilityJSON(t, map[string]any{"devices": []any{map[string]string{"path": filepath.Join(workspace, "missing")}}}),
		"regular":          capabilityJSON(t, map[string]any{"devices": []any{map[string]string{"path": regular}}}),
		"directory":        capabilityJSON(t, map[string]any{"devices": []any{map[string]string{"path": directory}}}),
		"fifo":             capabilityJSON(t, map[string]any{"devices": []any{map[string]string{"path": fifo}}}),
		"socket":           capabilityJSON(t, map[string]any{"devices": []any{map[string]string{"path": socket}}}),
		"symlink":          capabilityJSON(t, map[string]any{"devices": []any{map[string]string{"path": symlink}}}),
		"unknown field":    json.RawMessage(`{"devices":[{"path":"/dev/null","kind":"character"}]}`),
		"duplicate field":  json.RawMessage(`{"devices":[{"path":"/dev/null","path":"/dev/zero"}]}`),
		"malformed nested": json.RawMessage(`{"devices":[{"path":7}]}`),
	}
	for name, raw := range tests {
		t.Run(name, func(t *testing.T) {
			review := EvaluateCapability(context.Background(), CapabilityInput{
				Base: Spec{Mode: "enforce"}, Workspace: workspace, Raw: raw,
			}).Review()
			if review.State != CapabilitySoftDenied || review.Diagnostic == "" || !capabilitySetEmpty(review.EffectiveDelta) {
				t.Fatalf("review = %#v, want atomic diagnosed soft denial", review)
			}
		})
	}
}

func TestEvaluateDeviceCapabilityRecordsBlockIdentityWhenAvailable(t *testing.T) {
	var block string
	_ = filepath.WalkDir("/dev", func(path string, entry os.DirEntry, err error) error {
		if err != nil || block != "" {
			return nil
		}
		info, err := entry.Info()
		if err == nil && info.Mode()&os.ModeDevice != 0 && info.Mode()&os.ModeCharDevice == 0 {
			block = path
		}
		return nil
	})
	if block == "" {
		t.Skip("host exposes no block device under /dev")
	}
	review := EvaluateCapability(context.Background(), CapabilityInput{
		Base: Spec{Mode: "off"}, Workspace: t.TempDir(),
		Raw: capabilityJSON(t, map[string]any{"devices": []any{map[string]string{"path": block}}}),
	}).Review()
	if len(review.Request.Devices) != 1 || review.Request.Devices[0].Kind != CapabilityBlockDevice {
		t.Fatalf("review = %#v, want normalized block device", review)
	}
}

func TestCapabilityDeviceFromStatDeterministicallyRecordsBlockIdentity(t *testing.T) {
	device, err := capabilityDeviceFromStat("/dev/fabricated-block", unix.S_IFBLK|0o600, unix.Mkdev(259, 17))
	if err != nil {
		t.Fatal(err)
	}
	if device.Kind != CapabilityBlockDevice || device.Major != 259 || device.Minor != 17 {
		t.Fatalf("device = %#v, want block 259:17", device)
	}
}

func TestDeviceCapabilityRevalidationDetectsMajorMinorReplacement(t *testing.T) {
	device, err := InspectCapabilityDevice("/dev/null")
	if err != nil {
		t.Skipf("/dev/null is unavailable: %v", err)
	}
	device.Minor++
	if err := revalidateCapabilityDevice(device); err == nil || !strings.Contains(err.Error(), "changed identity") {
		t.Fatalf("revalidation error = %v, want changed identity", err)
	}
}

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

func TestLinuxDeviceCapabilityLaunchUsesPathStringDevBind(t *testing.T) {
	if !Available() {
		t.Skip("Bubblewrap unavailable")
	}
	assessment := EvaluateCapability(context.Background(), CapabilityInput{
		Base:      Spec{Mode: "enforce", Network: true, MinimalWrites: true},
		Workspace: t.TempDir(),
		Raw: capabilityJSON(t, map[string]any{
			"devices": []any{map[string]string{"path": "/dev/null"}},
		}),
	})
	if review := assessment.Review(); review.State != CapabilityReady {
		t.Skipf("device capability unavailable: state=%v diagnostic=%q", review.State, review.Diagnostic)
	}
	launch := PrepareCapabilityCommand(context.Background(), assessment, AuthorizedDelta, ResolveShell("bash", "", nil),
		"dd if=/dev/null of=/dev/null count=1 status=none")
	defer launch.Close()
	if !launch.UsesDelta || !containsArg(launch.Argv, "--dev-bind") {
		t.Fatalf("launch = %#v, want exact device delta", launch)
	}
	if launch.Materialization != CapabilityMaterializationPathStringDevBind {
		t.Fatalf("materialization = %v, want path-string device bind domain value", launch.Materialization)
	}
	disclosure := launch.Materialization.Disclosure()
	for _, required := range []string{"path-string --dev-bind", "accepted TOCTOU", "not descriptor-bound or race-free"} {
		if !strings.Contains(disclosure, required) {
			t.Fatalf("domain disclosure %q missing spec contract %q", disclosure, required)
		}
	}
	if !strings.Contains(launch.Diagnostic, disclosure) {
		t.Fatalf("prepared launch diagnostic %q missing domain disclosure %q", launch.Diagnostic, disclosure)
	}
	for _, arg := range launch.Argv {
		if strings.HasPrefix(arg, "/proc/self/fd/") {
			t.Fatalf("device launch falsely used proc fd source: %v", launch.Argv)
		}
	}
	if launch.Authority.Applied != CapabilityApplicationUnknown || !strings.Contains(launch.Diagnostic, "applied=unknown") {
		t.Fatalf("prepared launch claimed application: %#v diagnostic=%q", launch.Authority, launch.Diagnostic)
	}
	cmd := exec.Command(launch.Argv[0], launch.Argv[1:]...)
	cmd.ExtraFiles = launch.ExtraFiles
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("device capability failed: %v\n%s", err, out)
	}
	diagnostic := CapabilityExecutionDiagnostic(launch, CapabilityExecutionCompleted)
	if !strings.Contains(diagnostic, "applied=true") || launch.Materialization.Disclosure() == "" {
		t.Fatalf("execution diagnostic = %q", diagnostic)
	}
}

func TestLinuxActivationWitnessKeepsInterruptedMissingEventUnknown(t *testing.T) {
	makeLaunch := func(t *testing.T) CapabilityLaunch {
		t.Helper()
		activation, writer, err := newCapabilityActivationWitness()
		if err != nil {
			t.Fatal(err)
		}
		return CapabilityLaunch{
			ExtraFiles: []*os.File{writer}, UsesDelta: true, activation: activation,
			Authority: CapabilityAuthorityStatus{
				Requested: true, Supported: true, Prepared: true, Applied: CapabilityApplicationUnknown,
			},
		}
	}
	interrupted := makeLaunch(t)
	defer interrupted.Close()
	if diagnostic := CapabilityExecutionDiagnostic(interrupted, CapabilityExecutionInterrupted); !strings.Contains(diagnostic, "applied=unknown") {
		t.Fatalf("interrupted diagnostic = %q, missing witness must remain unknown", diagnostic)
	}

	completed := makeLaunch(t)
	defer completed.Close()
	if diagnostic := CapabilityExecutionDiagnostic(completed, CapabilityExecutionCompleted); !strings.Contains(diagnostic, "applied=false") {
		t.Fatalf("completed diagnostic = %q, missing witness without interruption must be false", diagnostic)
	}
}

func TestLinuxCompletedActivationWaitHasNoDeadline(t *testing.T) {
	if wait, bounded := capabilityActivationWait(CapabilityExecutionCompleted); bounded || wait != 0 {
		t.Fatalf("completed activation wait = (%s, %t), want deterministic unbounded drain", wait, bounded)
	}
	if wait, bounded := capabilityActivationWait(CapabilityExecutionInterrupted); !bounded || wait <= 0 {
		t.Fatalf("interrupted activation wait = (%s, %t), want positive bounded grace period", wait, bounded)
	}
}

func TestLinuxActivationWitnessBoundsStubbornOpenWriter(t *testing.T) {
	activation, writer, err := newCapabilityActivationWitness()
	if err != nil {
		t.Fatal(err)
	}
	duplicateFD, err := unix.Dup(int(writer.Fd()))
	if err != nil {
		t.Fatal(err)
	}
	stubbornWriter := os.NewFile(uintptr(duplicateFD), "stubborn-status-writer")
	launch := CapabilityLaunch{
		ExtraFiles: []*os.File{writer}, UsesDelta: true, activation: activation,
		Authority: CapabilityAuthorityStatus{
			Requested: true, Supported: true, Prepared: true, Applied: CapabilityApplicationUnknown,
		},
	}
	started := time.Now()
	if diagnostic := CapabilityExecutionDiagnostic(launch, CapabilityExecutionInterrupted); !strings.Contains(diagnostic, "applied=unknown") {
		t.Fatalf("interrupted diagnostic = %q", diagnostic)
	}
	launch.Close()
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("activation witness took %s with inherited writer still open", elapsed)
	}
	select {
	case <-activation.done:
	default:
		t.Fatal("activation drain goroutine did not stop after Close")
	}
	_ = stubbornWriter.Close()
}

func TestLinuxActivationWitnessProvesAppliedForNonzeroUserExit(t *testing.T) {
	if !Available() {
		t.Skip("Bubblewrap unavailable")
	}
	assessment := EvaluateCapability(context.Background(), CapabilityInput{
		Base: Spec{Mode: "enforce", Network: true, MinimalWrites: true}, Workspace: t.TempDir(),
		Raw: capabilityJSON(t, map[string]any{
			"devices": []any{map[string]string{"path": "/dev/null"}},
		}),
	})
	if review := assessment.Review(); review.State != CapabilityReady {
		t.Skipf("device capability unavailable: state=%v diagnostic=%q", review.State, review.Diagnostic)
	}
	launch := PrepareCapabilityCommand(context.Background(), assessment, AuthorizedDelta, ResolveShell("bash", "", nil), "exit 7")
	defer launch.Close()
	cmd := exec.Command(launch.Argv[0], launch.Argv[1:]...)
	cmd.ExtraFiles = launch.ExtraFiles
	if err := cmd.Run(); err == nil {
		t.Fatal("user command unexpectedly exited zero")
	}
	if diagnostic := CapabilityExecutionDiagnostic(launch, CapabilityExecutionCompleted); !strings.Contains(diagnostic, "applied=true") {
		t.Fatalf("diagnostic = %q, nonzero user exit must retain witnessed application", diagnostic)
	}
}

func TestLinuxActivationWitnessDoesNotTreatSetupFailureAsApplied(t *testing.T) {
	if !Available() {
		t.Skip("Bubblewrap unavailable")
	}
	assessment := EvaluateCapability(context.Background(), CapabilityInput{
		Base: Spec{Mode: "enforce", Network: true, MinimalWrites: true}, Workspace: t.TempDir(),
		Raw: capabilityJSON(t, map[string]any{
			"devices": []any{map[string]string{"path": "/dev/null"}},
		}),
	})
	if review := assessment.Review(); review.State != CapabilityReady {
		t.Skipf("device capability unavailable: state=%v diagnostic=%q", review.State, review.Diagnostic)
	}
	launch := PrepareCapabilityCommand(context.Background(), assessment, AuthorizedDelta, ResolveShell("bash", "", nil), "true")
	defer launch.Close()
	launch.Argv = append([]string{launch.Argv[0], "--reasonix-invalid-setup-option"}, launch.Argv[1:]...)
	cmd := exec.Command(launch.Argv[0], launch.Argv[1:]...)
	cmd.ExtraFiles = launch.ExtraFiles
	if err := cmd.Run(); err == nil {
		t.Fatal("invalid Bubblewrap setup unexpectedly succeeded")
	}
	diagnostic := CapabilityExecutionDiagnostic(launch, CapabilityExecutionCompleted)
	if !strings.Contains(diagnostic, "prepared=true") || !strings.Contains(diagnostic, "applied=false") {
		t.Fatalf("diagnostic = %q, setup failure must not claim application", diagnostic)
	}
}

func TestLinuxDeviceCapabilityIdentityChangeAtomicallyFallsBackMixedBundle(t *testing.T) {
	if !Available() {
		t.Skip("Bubblewrap unavailable")
	}
	assessment := EvaluateCapability(context.Background(), CapabilityInput{
		Base:      Spec{Mode: "enforce", MinimalWrites: true},
		Workspace: t.TempDir(),
		Raw: capabilityJSON(t, map[string]any{
			"network": true,
			"devices": []any{map[string]string{"path": "/dev/null"}},
		}),
	})
	if review := assessment.Review(); review.State != CapabilityReady {
		t.Skipf("mixed device capability unavailable: state=%v diagnostic=%q", review.State, review.Diagnostic)
	}
	assessment.review.EffectiveDelta.Devices[0].Major++
	launch := PrepareCapabilityCommand(context.Background(), assessment, AuthorizedDelta, ResolveShell("bash", "", nil), "true")
	defer launch.Close()
	if launch.UsesDelta || containsArg(launch.Argv, "--dev-bind") || !containsArg(launch.Argv, "--unshare-net") {
		t.Fatalf("launch = %#v, want unchanged base sandbox with no partial network/device delta", launch)
	}
	if !strings.Contains(launch.Diagnostic, "changed identity") || !strings.Contains(launch.Diagnostic, "prepared=false") {
		t.Fatalf("diagnostic = %q", launch.Diagnostic)
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
