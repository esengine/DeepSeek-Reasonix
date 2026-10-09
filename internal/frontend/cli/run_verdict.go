package cli

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"

	"reasonix/internal/contract/config"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/eventwire"
	"reasonix/internal/runtime/agent"
	"reasonix/internal/safety/permission"
	"reasonix/internal/session/control"
)

// runExitUnverified is `run --fail-on-unverified`'s exit status when the model
// finished but the host's final-readiness judgement was left unmet. It is
// apart from 1 (run error) and 2 (usage) so automation can tell them apart.
const runExitUnverified = 3

// runExitUntrustedFolder is the exit status of a run whose edits or commands
// were refused because the folder is untrusted, so the work was not done. It
// is apart from 1, 2 and 3; --fail-on-unverified keeps reporting 3 for it.
const runExitUntrustedFolder = 4

// runVerdict is what the host decided about the run beside its answer.
type runVerdict struct {
	completion *eventwire.CompletionSummary
}

func (v *runVerdict) observe(e event.Event) {
	if e.Kind == event.CompletionSummary && e.Completion != nil {
		v.completion = eventwire.ToWire(e).Completion
	}
}

// writeDenialWarning tells a text run's diagnostic stream which calls a
// permission gate refused; its stdout carries only the answer.
func writeDenialWarning(w io.Writer, denials []runPermissionDenial) {
	if w == nil || len(denials) == 0 {
		return
	}
	names := make([]string, 0, len(denials))
	for _, d := range denials {
		names = append(names, d.ToolName)
	}
	fmt.Fprintf(w, "warning: the permission policy refused %d tool call(s): %s\n", len(denials), strings.Join(names, ", "))
	seen := map[string]bool{}
	for _, d := range denials {
		if d.Remedy == "" || seen[d.Code] {
			continue
		}
		seen[d.Code] = true
		fmt.Fprintf(w, "warning: %s: the workspace folder %s; nothing that needed approval ran. To allow it, %s\n", d.Code, d.Cause, d.Remedy)
	}
}

// folderNote is what a refusal for an untrusted folder tells the person.
type folderNote struct{ cause, remedy string }

func newFolderNote(ctrl *control.Controller) folderNote {
	declined := ctrl.WorkspaceTrust() == config.WorkspaceTrustDeclined
	return folderNote{cause: control.UntrustedFolderCause(declined), remedy: control.UntrustedFolderRemedy(ctrl.WorkspaceRoot(), declined)}
}

// denialOf is the refusal a tool result reports, with the remedy its code has.
func denialOf(e event.Event, f folderNote) (runPermissionDenial, bool) {
	if e.Kind != event.ToolResult || !permission.IsRefusalCode(e.Tool.RefusalCode) {
		return runPermissionDenial{}, false
	}
	d := runPermissionDenial{ToolName: e.Tool.Name, ToolUseID: e.Tool.ID, Code: e.Tool.RefusalCode}
	if d.Code == permission.RefusalUntrustedFolder {
		d.Cause, d.Remedy = f.cause, f.remedy
	}
	return d, true
}

func unverifiedBy(denials []runPermissionDenial) string {
	if refusedByFolderTrust(denials) {
		return permission.RefusalUntrustedFolder
	}
	return ""
}

func refusedByFolderTrust(denials []runPermissionDenial) bool {
	for _, d := range denials {
		if d.Code == permission.RefusalUntrustedFolder {
			return true
		}
	}
	return false
}

// runDenialTally records refusals for a run whose sink prints no result object,
// so the plain text run can still say what it was not allowed to do.
type runDenialTally struct {
	event.AuditForwarder
	note    folderNote
	mu      sync.Mutex
	denials []runPermissionDenial
}

func newRunDenialTally(inner event.Sink) *runDenialTally {
	return &runDenialTally{AuditForwarder: event.AuditForwarder{Inner: inner}}
}

func (t *runDenialTally) setNote(n folderNote) {
	t.mu.Lock()
	t.note = n
	t.mu.Unlock()
}

func (t *runDenialTally) Emit(e event.Event) {
	t.mu.Lock()
	if d, ok := denialOf(e, t.note); ok {
		t.denials = append(t.denials, d)
	}
	t.mu.Unlock()
	t.Inner.Emit(e)
}

func (t *runDenialTally) snapshot() []runPermissionDenial {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]runPermissionDenial(nil), t.denials...)
}

// runReadiness is the unmet final-readiness judgement a run ended with, or nil.
func runReadiness(runErr error) *eventwire.FinalReadiness {
	var readiness *agent.FinalReadinessError
	if !errors.As(runErr, &readiness) || readiness == nil {
		return nil
	}
	return &eventwire.FinalReadiness{Attempts: readiness.Attempts, Missing: append([]string(nil), readiness.Missing...)}
}

// withFailOnUnverified applies --fail-on-unverified to the exit status only;
// the machine-readable result reports the same verdict either way.
func (c runCompletion) withFailOnUnverified(on bool) runCompletion {
	if on && c.unverified {
		c.exitCode = runExitUnverified
	}
	return c
}

// withFolderRefusal marks a run whose edits and commands were refused because
// the folder is untrusted as unverified: whatever the model then said, the work
// it was asked for did not happen.
func (c runCompletion) withFolderRefusal(refused bool) runCompletion {
	if refused && !c.isError {
		c.unverified = true
		c.exitCode = runExitUntrustedFolder
	}
	return c
}
