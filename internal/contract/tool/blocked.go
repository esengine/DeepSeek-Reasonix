package tool

import "errors"

// BlockedError is a host refusal raised by a tool that enforces its own
// execution policy — the call never ran. The agent renders it as a blocked
// outcome (the shape permission and plan-mode blocks already produce) rather
// than an execution failure, so loop guards count it and frontends can tell a
// refusal apart from a completed call.
type BlockedError struct{ Message string }

func (e *BlockedError) Error() string { return e.Message }

// Blocked returns a host refusal carrying the model-facing message.
func Blocked(msg string) error { return &BlockedError{Message: msg} }

// BlockedMessage reports whether err is a host refusal, and its message.
func BlockedMessage(err error) (string, bool) {
	var blocked *BlockedError
	if errors.As(err, &blocked) {
		return blocked.Message, true
	}
	return "", false
}
