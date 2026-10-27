package mutation

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
)

func decodeCoordinationRecord(data []byte, r *record) error {
	fields, err := coordinationMembers(data, []string{
		"version", "id", "sessionId", "executionId", "callId", "paths", "order", "active",
		"startedAt", "effect", "capacities", "workspace",
	})
	if err != nil {
		return err
	}
	if raw, ok := fields["capacities"]; ok {
		var capacities []json.RawMessage
		if err := json.Unmarshal(raw, &capacities); err != nil {
			return err
		}
		for _, capacity := range capacities {
			if _, err := coordinationMembers(capacity, []string{"id", "limit", "units"}); err != nil {
				return err
			}
		}
	}
	return json.Unmarshal(data, r)
}

// Struct decoding folds case and merges repeated members; validate their decoded
// names before either can change a protocol identity or resource declaration.
func coordinationMembers(data []byte, allowed []string) (map[string]json.RawMessage, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	opening, err := decoder.Token()
	if err != nil {
		return nil, err
	}
	if opening != json.Delim('{') {
		return nil, fmt.Errorf("record must be a JSON object: %w", ErrProtocol)
	}
	fields := make(map[string]json.RawMessage)
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		name, ok := token.(string)
		if !ok || !slices.Contains(allowed, name) {
			return nil, fmt.Errorf("unsupported record member %q: %w", name, ErrProtocol)
		}
		if _, exists := fields[name]; exists {
			return nil, fmt.Errorf("repeated record member %q: %w", name, ErrProtocol)
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return nil, err
		}
		fields[name] = value
	}
	if _, err := decoder.Token(); err != nil {
		return nil, err
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("record must contain one JSON value: %w", ErrProtocol)
	}
	return fields, nil
}
