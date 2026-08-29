package agent

import (
	"encoding/json"

	"reasonix/internal/provider"
)

// messageForSessionIdentity is the identity chokepoint shared by digests,
// prefix checks, and storage equality: re-round-tripping the message through
// the JSON encoder makes identity match the write/load form, so transcripts
// holding raw invalid UTF-8 bytes (mojibake tool output) no longer fork a
// recovery branch per save.
func messageForSessionIdentity(m provider.Message) provider.Message {
	// CreatedAt is local display metadata. Keep it out of transcript identity
	// so older builds that ignore the optional field can share the same event-
	// log revision and append without false conflicts.
	m.CreatedAt = 0
	if b, err := json.Marshal(m); err == nil {
		_ = json.Unmarshal(b, &m)
	}
	return m
}
