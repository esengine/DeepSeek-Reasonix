package config

import "reasonix/internal/provider/openai"

// migrateStoredDeepSeekEffort preserves the wire value of saved pre-contract
// aliases. New user selections still go through strict NormalizeEffort validation.
func migrateStoredDeepSeekEffort(e *ProviderEntry, effort string) string {
	if (effort == "medium" || effort == "xhigh") && (ReasoningProtocolForEntry(e) == ReasoningProtocolDeepSeek || (explicitReasoningProtocol(e) == "" && openai.IsDeepSeek(e.BaseURL))) && len(e.SupportedEfforts) == 0 {
		cap := ReasoningCapabilityForEntry(e)
		if cap.Validate(e.Model, "high") == nil {
			return "high"
		}
	}
	return effort
}
