// Hook configuration problems doctor can name without running anything.
package capdiag

import (
	"strings"

	"reasonix/internal/ext/hook"
)

func hookRuntimeIssue(entry hook.Entry, err error, disp func(string) string) (Issue, bool) {
	if err == nil {
		return Issue{}, false
	}
	return Issue{
		Severity: "error", Code: "hook.shell_unavailable", Subsystem: "hooks",
		Name: string(entry.Event), Source: disp(entry.Source),
		Message:     sanitizeErrText(err.Error()),
		Remediation: "Install Git for Windows, or configure [tools.shell] prefer=\"bash\" and path to a usable bash.exe, then re-run doctor capabilities",
		SettingsTab: "hooks",
	}, true
}

// hookPayloadVarIssue reports a hook that reads a Reasonix variable it is never
// given. The expansion is empty, so the test around it silently succeeds — a
// guard written this way passes every call instead of blocking one.
func hookPayloadVarIssue(entry hook.Entry, disp func(string) string) (Issue, bool) {
	undefined := hook.UndefinedPayloadVarsForEntry(entry)
	if len(undefined) == 0 {
		return Issue{}, false
	}
	return Issue{
		Severity: "warning", Code: "hook.undefined_payload_var", Subsystem: "hooks",
		Name: string(entry.Event), Source: disp(entry.Source),
		Message: "hook reads $" + strings.Join(undefined, ", $") + ", which Reasonix never sets; it expands to empty on every run",
		Remediation: "Read the payload instead of an environment variable — " + hook.PayloadDelivery +
			"; a test against the empty value passes silently, so a guard written this way never blocks",
		SettingsTab: "hooks",
	}, true
}
