package agent

import "reasonix/internal/provider"

// observeSummaryOutcome feeds a cache-aligned summary request's real token
// count back into prompt calibration. A provider overflow carries the exact
// prompt size the estimator missed, and a clean single-request success carries
// the same measurement for the history mix; the sampling path never sees
// either, so without this the next fold plan repeats the same misestimate.
func (a *Agent) observeSummaryOutcome(req provider.Request, usage *provider.Usage, err error) {
	if a == nil {
		return
	}
	if limit := provider.AsContextLimitError(err); limit != nil {
		if limit.PromptTokens > 0 {
			a.setPromptTokenCalibration(limit.PromptTokens, a.requestCalibrationShape(req))
		}
		if limit.WindowTokens > 0 {
			a.learnContextBudget(limit.WindowTokens, 0, false)
		}
		return
	}
	if err != nil || usage == nil || usage.Estimated || usage.Unknown || usage.RequestCount > 1 {
		return
	}
	if prompt := usage.LatestPromptTokens(); prompt > 0 {
		a.setPromptTokenCalibration(prompt, a.requestCalibrationShape(req))
	}
}
