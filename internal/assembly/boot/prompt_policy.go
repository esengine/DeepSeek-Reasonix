package boot

import "reasonix/internal/contract/config"

// corePolicies is every block appended to the base prompt at boot, in order.
// One list so a policy cannot be added to the prefix without the tests that
// strip it back off knowing about it.
var corePolicies = []string{
	config.UserDecisionPolicy,
	config.WorkPracticePolicy,
	config.ToolBatchPolicy,
	config.LanguagePolicy,
	config.InstructionDeliveryPolicy,
	config.ExternalContentPolicy,
}

func appendCorePolicies(prompt string) string {
	for _, policy := range corePolicies {
		prompt += "\n\n" + policy
	}
	return prompt
}

func appendOfflineEnvironmentNote(prompt string, offline bool) string {
	if offline {
		prompt += "\n\n" + config.OfflineEnvironmentNote
	}
	return prompt
}
