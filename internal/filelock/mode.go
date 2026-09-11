package filelock

// Mode selects exclusive vs shared advisory locking. ModeExclusive is the
// zero value so existing Acquire callers keep exclusive semantics.
type Mode int

const (
	ModeExclusive Mode = iota
	ModeShared
)
