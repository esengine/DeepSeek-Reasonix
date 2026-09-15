package boot

import "reasonix/internal/config"

func appendCorePolicies(prompt string, entry *config.ProviderEntry) string {
	for _, policy := range []string{config.UserDecisionPolicy, config.WorkPracticePolicy, config.LanguagePolicy} {
		prompt += "\n\n" + policy
	}
	// Per-model, and last: the action policy qualifies the core policies above
	// it, and only a model the operator opted in pays for the paragraph.
	return config.ApplyModelActionPolicy(prompt, entry)
}

func appendOfflineEnvironmentNote(prompt string, offline bool) string {
	if offline {
		prompt += "\n\n" + config.OfflineEnvironmentNote
	}
	return prompt
}
