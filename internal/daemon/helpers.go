package daemon

import "reasonix/internal/agent"

// saveRuntimeMeta is a thin helper wrapping agent.SaveRuntimeMeta so handler
// code reads cleanly.
func saveRuntimeMeta(sessionPath string, m agent.RuntimeMeta) error {
	return agent.SaveRuntimeMeta(sessionPath, m)
}
