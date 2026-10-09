package control

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"reasonix/internal/base/secrets"
)

// ErrMCPPromptFetch marks a prompt the user invoked whose text could not be
// obtained from its server.
var ErrMCPPromptFetch = errors.New("mcp prompt fetch failed")

const (
	mcpPromptStageGet     = "prompts/get"
	maxPromptFailuresOwed = 5
)

// MCPPromptError carries which prompt, which server and which stage failed;
// Err is the transport's own error, whose text the plugin layer already
// redacts.
type MCPPromptError struct {
	Prompt string
	Server string
	Stage  string
	Err    error
}

func (e *MCPPromptError) Error() string {
	return fmt.Sprintf("MCP prompt %q (server %q) failed at %s: %s", e.Prompt, e.Server, e.Stage, e.reason())
}

func (e *MCPPromptError) Unwrap() []error { return []error{ErrMCPPromptFetch, e.Err} }

func (e *MCPPromptError) reason() string {
	if e.Err == nil {
		return ""
	}
	return secrets.DiagnosticError(e.Err).Error()
}

var promptNoteEscaper = strings.NewReplacer("<", "&lt;", ">", "&gt;")

func (e *MCPPromptError) turnNote() string {
	return fmt.Sprintf("The user invoked MCP prompt %q (server %q); %s failed and no prompt text was received: %s",
		promptNoteEscaper.Replace(e.Prompt), promptNoteEscaper.Replace(e.Server), e.Stage, promptNoteEscaper.Replace(e.reason()))
}

// promptFailureDebt holds failures the model has not yet been told about.
// composed counts the entries the in-flight turn carries, so settling clears
// exactly those and never one recorded while the turn was being assembled.
type promptFailureDebt struct {
	mu       sync.Mutex
	notes    []string
	composed int
}

func (d *promptFailureDebt) owe(note string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.notes) >= maxPromptFailuresOwed {
		d.notes = d.notes[1:]
		d.composed = max(d.composed-1, 0)
	}
	d.notes = append(d.notes, note)
}

func (d *promptFailureDebt) owed() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.composed = len(d.notes)
	return strings.Join(d.notes, "\n")
}

func (d *promptFailureDebt) settle() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.notes = d.notes[d.composed:]
	d.composed = 0
}
