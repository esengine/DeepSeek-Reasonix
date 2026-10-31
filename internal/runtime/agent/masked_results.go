package agent

import "reasonix/internal/base/secrets"

// maskResults masks the values the host holds for the person in what a batch
// of tools returned, before it is drawn, stored or sent: a session's files are
// readable by other commands, and a tool reading .env should not put a value
// there for them to find.
func maskResults(results []string, outcomes []toolOutcome) {
	m := secrets.Masker()
	if m == nil {
		return
	}
	for i := range results {
		results[i] = m.String(results[i])
	}
	for i := range outcomes {
		outcomes[i].output = m.String(outcomes[i].output)
		outcomes[i].rawOutput = m.String(outcomes[i].rawOutput)
		outcomes[i].errMsg = m.String(outcomes[i].errMsg)
	}
}
