package plugin

import "testing"

func TestManagedCogneeToolAllowlistUsesProposalAPI(t *testing.T) {
	for _, name := range []string{"get_guidance", "search_memory", "propose_memory"} {
		if !managedCogneeToolAllowed(name) {
			t.Errorf("managed Cognee tool %q was rejected", name)
		}
	}
	for _, name := range []string{"remember", "recall", "forget", "propose_deletion", "check_proposal"} {
		if managedCogneeToolAllowed(name) {
			t.Errorf("managed Cognee tool %q was allowed", name)
		}
	}
}
