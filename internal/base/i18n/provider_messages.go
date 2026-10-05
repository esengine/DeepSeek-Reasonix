package i18n

// ProviderStatusMessage returns an actionable explanation for a known provider
// HTTP status, or "" when the status has no specific guidance.
func (m Messages) ProviderStatusMessage(status int) string {
	switch status {
	case 400:
		return m.ProviderErrBadRequest
	case 401, 403:
		return m.ProviderErrAuth
	case 402:
		return m.ProviderErrInsufficientBalance
	case 422:
		return m.ProviderErrUnprocessable
	case 429:
		return m.ProviderErrRateLimited
	case 500:
		return m.ProviderErrServer
	case 503:
		return m.ProviderErrServerBusy
	}
	return ""
}

// ProviderHintMessage returns the next step for a refusal the requesting client
// identified, or "" when it named none this catalogue answers. The hint arrives
// as its bare identity: this package sits below the provider layer and must not
// import it.
func (m Messages) ProviderHintMessage(hint string) string {
	switch hint {
	case "dropped_tool_call_reasoning":
		return m.ProviderErrDroppedReasoning
	}
	return ""
}
