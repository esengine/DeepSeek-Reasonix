package sandbox

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// CapabilityLaunch is one complete base or capability-derived command launch.
// ExtraFiles remain owned by the caller and must be closed after the child has
// inherited them or after launch aborts. UsesDelta means the argv represents the
// complete authorized delta; it is not a claim that the process has started.
type CapabilityLaunch struct {
	Argv       []string
	ExtraFiles []*os.File
	Wrapped    bool
	UsesDelta  bool
	Diagnostic string
}

// Close releases descriptor-bound mount sources held by the launch.
func (l *CapabilityLaunch) Close() {
	for _, file := range l.ExtraFiles {
		_ = file.Close()
	}
	l.ExtraFiles = nil
}

// PrepareCapabilityCommand materializes either the unchanged base sandbox or
// the complete authorized effective delta. Any validation or platform failure
// atomically falls back to the base command and returns a truthful diagnostic.
func PrepareCapabilityCommand(ctx context.Context, assessment CapabilityAssessment, use CapabilityUse, sh Shell, command string) CapabilityLaunch {
	baseArgv, baseWrapped := Command(assessment.base, sh, command)
	base := CapabilityLaunch{Argv: baseArgv, Wrapped: baseWrapped}
	if use != AuthorizedDelta || assessment.review.State != CapabilityReady {
		base.Diagnostic = CapabilityFallbackDiagnostic(assessment.review, use)
		return base
	}

	targets := append(append([]CapabilityPath(nil), assessment.review.EffectiveDelta.Reads...), assessment.review.EffectiveDelta.Writes...)
	for _, target := range targets {
		if err := revalidateCapabilityPath(assessment.workspace, target); err != nil {
			base.Diagnostic = fmt.Sprintf("sandbox capability request was not applied: %v", err)
			return base
		}
	}
	launch, err := prepareCapabilityPlatformLaunch(ctx, assessment.base, assessment.review.EffectiveDelta, sh, command)
	if err != nil {
		base.Diagnostic = fmt.Sprintf("sandbox capability request was not applied: %v", err)
		return base
	}
	launch.UsesDelta = true
	return launch
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
	if !review.Requested || review.State == CapabilityNoEffectiveDelta || review.State == CapabilityOmitted {
		return ""
	}
	if review.Diagnostic != "" {
		return "sandbox capability request was not applied: " + review.Diagnostic
	}
	if use == BaseOnly && review.State == CapabilityReady {
		return "sandbox capability request was not applied; command ran in the original sandbox"
	}
	return ""
}
