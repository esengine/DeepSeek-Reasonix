package agent

import (
	"encoding/json"

	"reasonix/internal/provider"
)

// CurrentSchemaVersion is the schema version written into every new session
// JSONL file as the first line (a header object). Bump this when the on-disk
// Message format changes in a backward-incompatible way and add a migration
// step in migrateMessages.
const CurrentSchemaVersion = 1

// SessionHeader is the first line of a v1+ JSONL session file. It is
// structurally orthogonal to provider.Message (which has no schema_version
// field), so LoadSession can distinguish headers from messages without
// ambiguity.
type SessionHeader struct {
	SchemaVersion int `json:"schema_version"`
}

// isHeaderLine reports whether raw looks like a session header object —
// a JSON object containing a "schema_version" key.
func isHeaderLine(raw json.RawMessage) bool {
	var probe struct {
		SchemaVersion *int `json:"schema_version"`
	}
	if err := json.Unmarshal(raw, &probe); err != nil {
		return false
	}
	return probe.SchemaVersion != nil
}

// decodeHeader extracts a SessionHeader from a raw JSON line.
func decodeHeader(raw json.RawMessage) SessionHeader {
	var h SessionHeader
	_ = json.Unmarshal(raw, &h)
	return h
}

// migrateMessages upgrades messages from the given schema version to
// CurrentSchemaVersion by applying each intermediate migration step in order.
// When from == CurrentSchemaVersion the input is returned unchanged.
// Add new migration steps here as the Message format evolves.
func migrateMessages(from int, msgs []provider.Message) []provider.Message {
	for v := from; v < CurrentSchemaVersion; v++ {
		switch v {
		// case 0: msgs = migrateV0toV1(msgs)
		// case 1: msgs = migrateV1toV2(msgs)
		// ...
		default:
			// Unknown future version — return as-is; the caller can log a warning.
			return msgs
		}
	}
	return msgs
}
