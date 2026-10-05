package sandbox

import (
	"context"
	"runtime"
	"slices"
	"strings"
	"testing"
)

// A launch with no session temp leases nothing, so there is nothing to point a
// child at. Exporting TMPDIR anyway would send it somewhere no one cleans up.
func TestPrepareArgsWithoutASessionTempExportsNothing(t *testing.T) {
	got := PrepareArgs(Spec{}, []string{"rg", "pattern"}, "")
	if got.SessionTemp != "" || got.EnvOverrides != nil {
		t.Fatalf("a launch with no lease carries %+v", got)
	}
	if got.LinuxSandboxed {
		t.Fatal("a launch with no lease reported a temp mapped into a sandbox")
	}
	if !slices.Equal(got.Argv, []string{"rg", "pattern"}) {
		t.Fatalf("argv = %v, want the arguments unchanged", got.Argv)
	}
}

// A spec that does not ask for confinement is not wrapped, whatever else it
// carries: the lease is about where temporary files go, not about who confines
// the process, and folding the two would confine on the strength of a temp dir.
func TestPrepareArgsLeasesATempWithoutConfining(t *testing.T) {
	const lease = "/leases/session-a"
	got := PrepareArgs(Spec{}, []string{"rg"}, lease)
	if got.Wrapped {
		t.Fatal("a spec that asked for no confinement was wrapped")
	}
	if got.SessionTemp != lease {
		t.Fatalf("SessionTemp = %q, want %q", got.SessionTemp, lease)
	}
	if got.LinuxSandboxed {
		t.Fatal("an unwrapped launch reported its temp mapped into a sandbox")
	}
	// Unwrapped means the child sees the host filesystem, so every key has to
	// name the host path. /tmp is only where the lease appears from inside
	// bubblewrap, and a child told that outside one writes somewhere else.
	if len(got.EnvOverrides) != len(SessionTempEnvKeys) {
		t.Fatalf("EnvOverrides = %v, want one per key", got.EnvOverrides)
	}
	for _, kv := range got.EnvOverrides {
		if _, value, _ := strings.Cut(kv, "="); value != lease {
			t.Fatalf("%s points somewhere other than the leased directory", kv)
		}
	}
}

// LinuxSandboxed is the conjunction of three things and reports so. It is read
// to decide whether the child is told /tmp or the host path, and a true on a
// platform that binds nothing is how a process is pointed at a directory that
// is not its own.
func TestPrepareArgsReportsTheMappingOnlyWhereItHappens(t *testing.T) {
	got := PrepareArgs(Spec{Mode: "enforce"}, []string{"rg"}, "/leases/session-a")
	if !got.Wrapped && got.LinuxSandboxed {
		t.Fatal("a launch nothing wrapped reported a bind into the wrapper")
	}
	if runtime.GOOS != "linux" && got.LinuxSandboxed {
		t.Fatalf("%s reported a bubblewrap bind", runtime.GOOS)
	}
	// Whatever the platform decided, the two must agree: the flag exists to
	// choose the value, so a mapping claimed and not reflected is the failure.
	want := got.SessionTemp
	if got.LinuxSandboxed {
		want = "/tmp"
	}
	for _, kv := range got.EnvOverrides {
		if _, value, _ := strings.Cut(kv, "="); value != want {
			t.Fatalf("%s disagrees with LinuxSandboxed=%v", kv, got.LinuxSandboxed)
		}
	}
}

type stubApprover struct{}

func (stubApprover) ApproveSandboxEscape(context.Context, EscapeRequest) (bool, string, error) {
	return true, "", nil
}

// Nothing carried means nobody to ask, and an escape with nobody to ask fails
// closed. Both ways of carrying nothing have to read the same: a nil approver
// is not stamped, and a typed nil inside a non-nil interface — the shape a
// caller gets from a helper that returned a nil pointer — is not an approver
// either, though `!= nil` on the interface would say it is.
func TestAnEscapeApproverThatIsNotThereIsNotFound(t *testing.T) {
	if _, ok := EscapeApproverFrom(context.Background()); ok {
		t.Fatal("a context nobody stamped answered with an approver")
	}
	if _, ok := EscapeApproverFrom(WithEscapeApprover(context.Background(), nil)); ok {
		t.Fatal("stamping nothing left something to ask")
	}
	var typedNil *nilApprover
	if _, ok := EscapeApproverFrom(WithEscapeApprover(context.Background(), typedNil)); ok {
		t.Fatal("a typed nil was accepted as somebody to ask")
	}
}

type nilApprover struct{}

func (*nilApprover) ApproveSandboxEscape(context.Context, EscapeRequest) (bool, string, error) {
	return true, "", nil
}

func TestAnEscapeApproverThatIsThereIsFound(t *testing.T) {
	ctx := WithEscapeApprover(context.Background(), stubApprover{})
	approver, ok := EscapeApproverFrom(ctx)
	if !ok || approver == nil {
		t.Fatal("the approver stamped on the context was not found")
	}
	// Derived contexts keep it: the approval is asked for deep inside a tool
	// call, not where it was stamped.
	child, cancel := context.WithCancel(ctx)
	defer cancel()
	if _, ok := EscapeApproverFrom(child); !ok {
		t.Fatal("the approver did not survive a derived context")
	}
}

// The code is the identity and the sentence is one rendering of it. They are
// separate so a frontend can say it in the reader's language; what has to hold
// is that this platform has both, and that the code is a code rather than the
// sentence again.
func TestTheUnavailableRefusalCarriesAnIdentityAndAHint(t *testing.T) {
	code := UnavailableCode()
	if code == "" {
		t.Fatal("an unavailable sandbox refuses with no identity to say")
	}
	if !strings.HasPrefix(code, "sandbox.") || strings.Contains(code, " ") {
		t.Fatalf("UnavailableCode = %q, want a dotted code rather than a sentence", code)
	}
	remediation := UnavailableRemediation()
	if remediation == "" {
		t.Fatal("an unavailable sandbox names no way out of it")
	}
	if strings.Contains(remediation, code) {
		t.Fatalf("the hint repeats the code: %q", remediation)
	}
	// The message is the hint with the refusal in front, so a surface that shows
	// one of them never shows half a sentence.
	if !strings.HasSuffix(UnavailableMessage(), remediation) {
		t.Fatalf("UnavailableMessage does not end in its own remediation: %q", UnavailableMessage())
	}
}
