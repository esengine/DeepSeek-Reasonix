package event

// RetryDetail carries optional, body-free status metadata for Retrying events.
type RetryDetail struct {
	RetryReason  string
	RetryDelayMs int64
}
