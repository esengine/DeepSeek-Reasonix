package agent

import "fmt"

// checkpointRejection carries why a fold was judged not worth installing. The
// reason used to live only in the sentence, and the reader cut it back out at
// the sentinel's own wording — so rewording the sentinel silently returned the
// whole error where a reason was expected.
type checkpointRejection struct {
	code   CompactionNoopReason
	reason string
}

func (c *checkpointRejection) Error() string {
	return errCheckpointRejected.Error() + ": " + c.reason
}

func (c *checkpointRejection) Unwrap() error { return errCheckpointRejected }

// rejectCheckpoint builds the verdict with the rule that refused it and the
// figures behind that rule.
func rejectCheckpoint(code CompactionNoopReason, format string, args ...any) error {
	return &checkpointRejection{code: code, reason: fmt.Sprintf(format, args...)}
}
