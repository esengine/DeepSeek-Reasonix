package builtin

import (
	"encoding/json"
	"strings"
	"testing"
)

// The schema is the contract the model reads, so it must not promise a wait the
// runtime will not perform. The description states the cap and that omitting the
// timeout is capped too, because clampWaitSeconds applies it in both cases.
func TestWaitSchemaStatesTheSameBoundTheRuntimeApplies(t *testing.T) {
	var schema struct {
		Properties map[string]struct {
			Type        string `json:"type"`
			Description string `json:"description"`
			Maximum     int    `json:"maximum"`
		} `json:"properties"`
	}
	if err := json.Unmarshal((waitJob{}).Schema(), &schema); err != nil {
		t.Fatal(err)
	}
	timeout := schema.Properties["timeout_seconds"]
	if timeout.Maximum != waitCeilingSeconds {
		t.Fatalf("schema maximum = %d, want the runtime ceiling %d", timeout.Maximum, waitCeilingSeconds)
	}
	if !strings.Contains(timeout.Description, "capped at") || !strings.Contains(timeout.Description, "omitted") {
		t.Fatalf("description = %q, want it to say the cap applies when the timeout is omitted too", timeout.Description)
	}
	// The old wording promised an unbounded wait, which the runtime no longer does.
	if strings.Contains(timeout.Description, "blocks until finish") {
		t.Fatalf("description = %q still promises an unbounded wait", timeout.Description)
	}
}
