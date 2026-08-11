package agent

// contextUsage memoises the projected prompt size. The estimate walks every
// visible message, and status gauges redraw far more often than the view moves,
// so it is keyed on the three things that can change the answer: the
// transcript, the projection, and the calibration.
type contextUsage struct {
	transcriptVersion uint64
	projectionVersion uint64
	calibration       *promptTokenCalibration
	tokens            int
}

// ContextUsedTokens is the number ContextManager compares against its
// thresholds: the estimated prompt size of the view the next request sends. A
// gauge fed from the last turn's usage instead lags a turn, counts completion
// tokens the trigger ignores, and reads zero on a rebound session — which is
// how a session displays 8% while it is compacting.
func (a *Agent) ContextUsedTokens() int {
	if a == nil {
		return 0
	}
	session := a.Session()
	if session == nil {
		return 0
	}
	transcriptVersion := session.TranscriptVersion()
	projectionVersion := a.currentProjectionVersion()
	calibration := a.promptCalibration.Load()
	if cached := a.contextUsage.Load(); cached != nil &&
		cached.transcriptVersion == transcriptVersion &&
		cached.projectionVersion == projectionVersion &&
		cached.calibration == calibration {
		return cached.tokens
	}
	tokens := a.estimatedPromptTokens(a.modelVisibleMessages())
	a.contextUsage.Store(&contextUsage{
		transcriptVersion: transcriptVersion,
		projectionVersion: projectionVersion,
		calibration:       calibration,
		tokens:            tokens,
	})
	return tokens
}
