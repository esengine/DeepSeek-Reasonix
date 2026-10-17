package agentcomposition

import "strings"

// DiagnosticError preserves module identities across transport boundaries.
type DiagnosticError struct{ Diagnostics []Diagnostic }

func (e *DiagnosticError) Error() string {
	lines := []string{ErrUnavailable.Error()}
	for _, d := range e.Diagnostics {
		lines = append(lines, d.Module+": "+d.Detail)
	}
	return strings.Join(lines, "; ")
}
func (e *DiagnosticError) Unwrap() error { return ErrUnavailable }
func Unavailable(module, detail string) error {
	return &DiagnosticError{Diagnostics: []Diagnostic{{Code: "agent_composition.unavailable", Module: module, Detail: detail}}}
}
