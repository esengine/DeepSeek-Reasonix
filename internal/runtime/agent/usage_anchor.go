package agent

import "reasonix/internal/contract/provider"

// usageAnchor ties one provider-reported prompt size to the visible view that
// request carried. promptTokens stays zero until the usage arrives, so a request
// whose stream ended without one leaves no anchor to read.
type usageAnchor struct {
	// promptTokens is the request's prompt_tokens as the provider counted it,
	// cached tokens included (every provider adapter normalises to that).
	promptTokens      int
	visible           requestCalibrationShape
	projectionVersion uint64
}

// anchorProviderUsage completes the anchor of the request in flight. It trusts
// the same usage as calibration does, minus the image rule: pixels the host
// cannot count are exactly what the anchor is for. A turn the provider padded
// with its own tool results counts pages no fold can remove, so it anchors nothing.
func (a *contextWindow) anchorProviderUsage(usage *provider.Usage) {
	if a == nil || usage == nil || usage.Estimated || usage.ServerToolRequests > 0 {
		return
	}
	tokens := usage.LatestPromptTokens()
	sent := a.sess.output.usageAnchor.Load()
	if tokens <= 0 || sent == nil || sent.visible.requestChars <= 0 {
		return
	}
	anchored := *sent
	anchored.promptTokens = tokens
	a.sess.output.usageAnchor.Store(&anchored)
}

// providerReportedFloor is the provider's last prompt size plus what the visible
// view has gained since that request, both sides of the gain sized by the same
// local estimate so a stale ratio cancels. It answers false after any fold: the
// reported size describes a context that no longer exists, and only the next
// request's usage anchors the new one.
func (a *contextWindow) providerReportedFloor(visible requestCalibrationShape, projectionVersion uint64) (int, bool) {
	if a == nil {
		return 0, false
	}
	anchor := a.sess.output.usageAnchor.Load()
	if anchor == nil || anchor.promptTokens <= 0 || anchor.projectionVersion != projectionVersion {
		return 0, false
	}
	gain := a.estimatedShapeTokens(visible) - a.estimatedShapeTokens(anchor.visible)
	return max(anchor.promptTokens+gain, 0), true
}
