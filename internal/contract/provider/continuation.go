package provider

// ContinuationRecoveryError preserves the final failure and the host's retry action.
type ContinuationRecoveryError struct {
	Err error
}

func (e *ContinuationRecoveryError) Error() string {
	return "Responses continuation rejected (HTTP 400); retry with full history without previous_response_id failed: " + e.Err.Error()
}

func (e *ContinuationRecoveryError) Unwrap() error { return e.Err }
