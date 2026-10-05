// computer.go — reading and operating another application on this machine.
package permission

import "strings"

// ComputerPointerPrefix marks a subject whose steps move the person's own
// pointer rather than asking an element to act. The host writes it; a grant for
// the application does not answer it, because the two are different things to
// agree to.
const ComputerPointerPrefix = "pointer:"

// ComputerSubjectTakesPointer reports a subject a person answers for even when
// the application it names is already granted.
func ComputerSubjectTakesPointer(subject string) bool {
	return strings.HasPrefix(subject, ComputerPointerPrefix)
}

// computerSubjects is every subject a computer call answers to: taking the
// pointer over an application is also operating that application, so a rule
// refusing the application refuses it.
func computerSubjects(subject string) []string {
	if app, ok := strings.CutPrefix(subject, ComputerPointerPrefix); ok && app != "" {
		return []string{subject, app}
	}
	return []string{subject}
}

// IsComputerTool reports whether a tool reads or operates another application.
func IsComputerTool(toolName string) bool { return groupOf(toolName) == computerGroup }

// decideComputer answers a call on another application, named by its bundle
// id. Input there reaches whatever that application can, so only a rule naming
// the application answers it — never a glob, a bare grant or auto.
func (p Policy) decideComputer(toolName, subject string) (Decision, bool) {
	if !IsComputerTool(toolName) {
		return 0, false
	}
	switch {
	case matchAny(p.Deny, toolName, subject):
		return Deny, true
	case ComputerSubjectTakesPointer(subject) && (matchAnyExact(p.SessionAllow, toolName, subject) || matchAnyExact(p.Allow, toolName, subject)):
		return Allow, true
	case ComputerSubjectTakesPointer(subject):
		if p.Mode == Deny {
			return Deny, true
		}
		return Ask, true
	case matchAnyExact(p.SessionAllow, toolName, subject), matchAnyExact(p.Allow, toolName, subject):
		return Allow, true
	case p.Mode == Deny:
		return Deny, true
	}
	return Ask, true
}
