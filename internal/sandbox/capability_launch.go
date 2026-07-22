package sandbox

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// CapabilityMaterialization identifies the enforcement primitive selected for
// a prepared delta. Diagnostics are rendered centrally from this domain value.
type CapabilityMaterialization uint8

const (
	// CapabilityMaterializationNone carries no special disclosure.
	CapabilityMaterializationNone CapabilityMaterialization = iota
	// CapabilityMaterializationPathStringDevBind uses Bubblewrap's path-string
	// device bind and therefore carries the issue #10 accepted TOCTOU window.
	CapabilityMaterializationPathStringDevBind
)

// Disclosure returns the stable diagnostic contract for this enforcement
// primitive. Callers render this value rather than maintaining free-form text.
func (m CapabilityMaterialization) Disclosure() string {
	switch m {
	case CapabilityMaterializationNone:
		return ""
	case CapabilityMaterializationPathStringDevBind:
		return "path-string --dev-bind (accepted TOCTOU; not descriptor-bound or race-free)"
	default:
		return fmt.Sprintf("unknown(%d)", m)
	}
}

// CapabilityExecutionOutcome records whether process termination may have
// prevented the backend from emitting its final activation witness.
type CapabilityExecutionOutcome uint8

const (
	// CapabilityExecutionCompleted means the backend process reached a normal
	// terminal result, including a nonzero exit, even if caller cancellation or
	// timeout raced with that result.
	CapabilityExecutionCompleted CapabilityExecutionOutcome = iota
	// CapabilityExecutionInterrupted means cancellation, timeout, or forced
	// termination may have removed a witness after authority was already active.
	CapabilityExecutionInterrupted
)

// CapabilityLaunch is one complete base or capability-derived command launch.
// ExtraFiles remain owned by the caller and must be closed after the child has
// inherited them or after launch aborts. UsesDelta means the argv represents the
// complete authorized delta; it is not a claim that the process has started.
type CapabilityLaunch struct {
	Argv            []string
	ExtraFiles      []*os.File
	Wrapped         bool
	UsesDelta       bool
	Authority       CapabilityAuthorityStatus
	Materialization CapabilityMaterialization
	Diagnostic      string
	activation      *capabilityActivationWitness
}

// Close releases descriptor-bound mount sources held by the launch.
func (l *CapabilityLaunch) Close() {
	for _, file := range l.ExtraFiles {
		_ = file.Close()
	}
	l.ExtraFiles = nil
	if l.activation != nil {
		l.activation.close()
		l.activation = nil
	}
}

// PrepareCapabilityCommand materializes either the unchanged base sandbox or
// the complete authorized effective delta. Any validation or platform failure
// atomically falls back to the base command and returns a truthful diagnostic.
func PrepareCapabilityCommand(ctx context.Context, assessment CapabilityAssessment, use CapabilityUse, sh Shell, command string) CapabilityLaunch {
	baseArgv, baseWrapped := Command(assessment.base, sh, command)
	base := CapabilityLaunch{Argv: baseArgv, Wrapped: baseWrapped, Authority: assessment.review.Authority}
	if use != AuthorizedDelta || assessment.review.State != CapabilityReady {
		base.Diagnostic = CapabilityFallbackDiagnostic(assessment.review, use)
		return base
	}

	targets := append(append([]CapabilityPath(nil), assessment.review.EffectiveDelta.Reads...), assessment.review.EffectiveDelta.Writes...)
	for _, target := range targets {
		if err := revalidateCapabilityPath(assessment.workspace, target); err != nil {
			base.Diagnostic = fmt.Sprintf("sandbox capability request was not applied: %v; %s", err, formatCapabilityAuthority(base.Authority))
			return base
		}
	}
	for _, device := range assessment.review.EffectiveDelta.Devices {
		if err := revalidateCapabilityDevice(device); err != nil {
			base.Diagnostic = fmt.Sprintf("sandbox capability request was not applied: %v; %s", err, formatCapabilityAuthority(base.Authority))
			return base
		}
	}
	launch, err := prepareCapabilityPlatformLaunch(ctx, assessment.base, assessment.review.EffectiveDelta, sh, command)
	if err != nil {
		base.Diagnostic = fmt.Sprintf("sandbox capability request was not applied: %v; %s", err, formatCapabilityAuthority(base.Authority))
		return base
	}
	launch.UsesDelta = true
	launch.Authority = assessment.review.Authority
	launch.Authority.Prepared = true
	launch.Authority.Applied = CapabilityApplicationUnknown
	launch.Diagnostic = formatPreparedCapabilityDiagnostic(launch)
	return launch
}

func revalidateCapabilityDevice(expected CapabilityDevice) error {
	real, err := filepath.EvalSymlinks(expected.Canonical)
	if err != nil {
		return fmt.Errorf("revalidate device %q: %w", expected.Canonical, err)
	}
	if filepath.Clean(real) != expected.Canonical {
		return fmt.Errorf("device %q changed canonical identity to %q", expected.Canonical, real)
	}
	actual, err := inspectCapabilityDevice(expected.Canonical)
	if err != nil {
		return fmt.Errorf("revalidate device %q: %w", expected.Canonical, err)
	}
	if actual.Kind != expected.Kind || actual.Major != expected.Major || actual.Minor != expected.Minor {
		return fmt.Errorf("device %q changed identity from %s %d:%d to %s %d:%d",
			expected.Canonical, expected.Kind, expected.Major, expected.Minor,
			actual.Kind, actual.Major, actual.Minor)
	}
	return nil
}

func revalidateCapabilityPath(workspace string, expected CapabilityPath) error {
	candidate := expected.Path
	if expected.Identity == WorkspaceRelative {
		candidate = filepath.Join(workspace, expected.Path)
	}
	real, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return fmt.Errorf("revalidate %q: %w", expected.Path, err)
	}
	real = filepath.Clean(real)
	if real != expected.Canonical {
		return fmt.Errorf("path %q changed identity from %q to %q", expected.Path, expected.Canonical, real)
	}
	if expected.Identity == WorkspaceRelative && !pathAtOrBelow(real, workspace) {
		return fmt.Errorf("path %q escaped the workspace", expected.Path)
	}
	info, err := os.Stat(real)
	if err != nil {
		return fmt.Errorf("stat %q: %w", expected.Path, err)
	}
	kind, err := capabilityPathKind(info)
	if err != nil {
		return fmt.Errorf("path %q: %w", expected.Path, err)
	}
	if kind != expected.Kind {
		return fmt.Errorf("path %q changed kind from %s to %s", expected.Path, expected.Kind, kind)
	}
	return nil
}

// CapabilityFallbackDiagnostic returns the authoritative user-facing reason
// that a requested bundle did not add authority. An empty result means no
// fallback needs to be surfaced.
func CapabilityFallbackDiagnostic(review CapabilityReview, use CapabilityUse) string {
	if !review.Authority.Requested || review.State == CapabilityOmitted {
		return ""
	}
	if review.Diagnostic != "" {
		return "sandbox capability request was not applied: " + review.Diagnostic + "; " + formatCapabilityAuthority(review.Authority)
	}
	if use == BaseOnly && review.State == CapabilityReady {
		return "sandbox capability request was not applied; command ran in the original sandbox; " + formatCapabilityAuthority(review.Authority)
	}
	if review.State == CapabilityNoEffectiveDelta {
		if len(review.Request.Devices) == 0 {
			return ""
		}
		return "sandbox capability request added no OS authority; " + formatCapabilityAuthority(review.Authority)
	}
	return ""
}

// CapabilityExecutionDiagnostic reports the backend's activation witness. It
// never infers application from the user command's exit status.
func CapabilityExecutionDiagnostic(launch CapabilityLaunch, outcome CapabilityExecutionOutcome) string {
	if !launch.Authority.Requested {
		return launch.Diagnostic
	}
	status := launch.Authority
	if launch.UsesDelta && launch.activation != nil {
		status.Applied = launch.activation.state(outcome)
	}
	diagnostic := formatCapabilityAuthority(status)
	if disclosure := launch.Materialization.Disclosure(); disclosure != "" {
		diagnostic += "; materialization=" + disclosure
	}
	return "sandbox capability status: " + diagnostic
}

func formatPreparedCapabilityDiagnostic(launch CapabilityLaunch) string {
	status := launch.Authority
	diagnostic := formatCapabilityAuthority(status)
	if disclosure := launch.Materialization.Disclosure(); disclosure != "" {
		diagnostic += "; materialization=" + disclosure
	}
	return "sandbox capability status: " + diagnostic
}

func formatCapabilityAuthority(status CapabilityAuthorityStatus) string {
	return fmt.Sprintf("requested=%t supported=%t prepared=%t applied=%s",
		status.Requested, status.Supported, status.Prepared, status.Applied)
}
