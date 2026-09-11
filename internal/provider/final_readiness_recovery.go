package provider

import "encoding/json"

// FinalReadinessRecovery stores provider-excluded recovery state. Checkpoint
// stays opaque here to avoid coupling provider types to the evidence package.
type FinalReadinessRecovery struct {
	Pending    bool            `json:"pending,omitempty"`
	Missing    []string        `json:"missing,omitempty"`
	Checkpoint json.RawMessage `json:"checkpoint,omitempty"`
}
