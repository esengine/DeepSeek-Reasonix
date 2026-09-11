package main

import (
	"bytes"
	"encoding/json"
	"fmt"

	"reasonix/internal/event"
)

// Decode before converting to value fields: missing false/zero facts are not
// observations of an idle runtime. Unknown extension fields remain compatible.
func decodeRemoteRuntimeState(data json.RawMessage) (event.RuntimeStateSnapshot, error) {
	var fields map[string]json.RawMessage
	var state event.RuntimeStateSnapshot
	if err := json.Unmarshal(data, &fields); err != nil {
		return state, err
	}
	for _, key := range []string{"schemaVersion", "runtimeEpoch", "revision", "phase", "running", "pendingPrompt", "cancelRequested", "cancellable", "backgroundJobs"} {
		value, found := fields[key]
		if !found || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return state, fmt.Errorf("runtime state missing %s", key)
		}
	}
	if err := json.Unmarshal(data, &state); err != nil {
		return state, err
	}
	if !validRuntimeState(state) {
		return state, fmt.Errorf("invalid runtime state")
	}
	return state, nil
}

func (p *remoteTabStatusPayload) UnmarshalJSON(data []byte) error {
	type plain remoteTabStatusPayload
	var wire struct {
		*plain
		State json.RawMessage `json:"runtimeState"`
	}
	wire.plain = (*plain)(p)
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}
	if len(wire.State) == 0 {
		return nil
	}
	state, err := decodeRemoteRuntimeState(wire.State)
	if err != nil {
		return err
	}
	p.RuntimeState = &state
	return nil
}
