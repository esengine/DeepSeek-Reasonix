package boot

import (
	"testing"
)

// Declaring a conditional tool must not put it in a session that was not
// offered it: the request's tool list is the cache-stable prefix.
func TestEffectConditionalDeclarationsDoNotReachTheProviderPrefix(t *testing.T) {
	isolateConfigHome(t)
	t.Chdir(robustTempDir(t))
	writeUserConfig(t, userModel)
	req := firstTokenProfileRequest(t, "")
	sent := map[string]bool{}
	for _, schema := range req.Tools {
		sent[schema.Name] = true
	}
	for _, n := range []string{"advise", "best_of_n", "apply_isolated", "discard_isolated", "system_one", "submit_plan", "conclude_no_changes", "review_report"} {
		if sent[n] {
			t.Errorf("%s is declared conditional but reached the default request tools", n)
		}
	}
}
