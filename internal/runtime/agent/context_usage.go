package agent

import "reasonix/internal/contract/tool"

// contextUsage memoises the projected prompt size. The estimate walks every
// visible message, and status gauges redraw far more often than the view moves,
// so it is keyed on everything that can change the answer: the transcript,
// projection, calibration, and provider-visible tool schemas.
type contextUsage struct {
	transcriptVersion  uint64
	projectionVersion  uint64
	calibration        *promptTokenCalibration
	tools              *tool.Registry
	toolSchemaRevision uint64
	tokens             int
}

// ContextUsedTokens is the local estimate of the prompt size the next request
// sends. The fold trigger also reads the provider's last reported size
// (providerReportedFloor); the hard ceiling reads this number alone. A gauge fed
// from the last turn's usage lags a turn and reads zero on a rebound session.
func (a *Agent) ContextUsedTokens() int {
	if a == nil {
		return 0
	}
	return a.window().contextUsedTokens()
}

func (a *contextWindow) contextUsedTokens() int {
	session := a.Session()
	if session == nil {
		return 0
	}
	transcriptVersion := session.TranscriptVersion()
	projectionVersion := a.currentProjectionVersion()
	calibration := a.sess.output.promptCalibration.Load()
	tools := a.svc.tools
	toolSchemaRevision := tools.SchemaRevision()
	if cached := a.sess.output.contextUsage.Load(); cached != nil &&
		cached.transcriptVersion == transcriptVersion &&
		cached.projectionVersion == projectionVersion &&
		cached.calibration == calibration &&
		cached.tools == tools &&
		cached.toolSchemaRevision == toolSchemaRevision {
		return cached.tokens
	}
	tokens := a.estimatedVisibleRequestTokens(a.modelVisibleMessages())
	a.sess.output.contextUsage.Store(&contextUsage{
		transcriptVersion:  transcriptVersion,
		projectionVersion:  projectionVersion,
		calibration:        calibration,
		tools:              tools,
		toolSchemaRevision: toolSchemaRevision,
		tokens:             tokens,
	})
	return tokens
}
