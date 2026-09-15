package notify

import (
	"fmt"
	"io"
	"os"
	"strings"
)

// TerminalSender sends notifications directly to a terminal stream via
// ASCII Bell and terminal escape sequences (OSC 777 and OSC 9).
// It delivers attention cues and notifications in both local and remote/SSH
// sessions without relying on host-level notification daemons.
type TerminalSender struct {
	Out io.Writer
}

// NewTerminalSender creates a TerminalSender writing to os.Stderr by default.
func NewTerminalSender() *TerminalSender {
	return &TerminalSender{Out: os.Stderr}
}

// Send outputs an ASCII bell and standard terminal notification sequences.
func (t *TerminalSender) Send(m Message) error {
	w := t.Out
	if w == nil {
		w = os.Stderr
	}

	title := sanitizeTerminalString(m.Title)
	body := sanitizeTerminalString(m.Body)

	// 1. ASCII Bell (\a): triggers tab flash / alert in virtually all terminals.
	// 2. OSC 777: \x1b]777;notify;<title>;<body>\x1b\\ (Ghostty, WezTerm, Alacritty, Windows Terminal, etc.)
	// 3. OSC 9: \x1b]9;<body>\x1b\\ (iTerm2, ConEmu, Mintty, etc.)
	var seq strings.Builder
	seq.WriteString("\a")
	if title != "" || body != "" {
		fmt.Fprintf(&seq, "\x1b]777;notify;%s;%s\x1b\\", title, body)
		fmt.Fprintf(&seq, "\x1b]9;%s\x1b\\", body)
	}

	_, err := io.WriteString(w, seq.String())
	return err
}

// sanitizeTerminalString strips ASCII control characters to prevent escape injection.
func sanitizeTerminalString(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r >= 32 && r != 127 {
			b.WriteRune(r)
		} else if r == '\t' || r == '\n' {
			b.WriteRune(' ')
		}
	}
	return strings.TrimSpace(b.String())
}

// MultiSender delivers a notification to all non-nil child senders.
type MultiSender []Sender

// Send forwards m to all child senders and returns the first error encountered, if any.
func (ms MultiSender) Send(m Message) error {
	var firstErr error
	for _, s := range ms {
		if s == nil {
			continue
		}
		if err := s.Send(m); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
