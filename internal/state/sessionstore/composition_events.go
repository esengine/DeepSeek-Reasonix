package sessionstore

import "reasonix/internal/contract/provider"

const sessionEventCompositionSchemaVersion = 4

func compositionEventSchema(path string, msgs []provider.Message, images []sessionEventImages) int {
	for _, m := range msgs {
		if m.AgentComposition != nil {
			return sessionEventCompositionSchemaVersion
		}
	}
	if probe, err := probeSessionEventLog(path); err == nil && probe.schemaVersion == sessionEventCompositionSchemaVersion {
		return sessionEventCompositionSchemaVersion
	}
	return sessionEventRecordSchema(images)
}
