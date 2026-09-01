package eventwire

import (
	"encoding/json"
	"errors"
	"testing"

	"reasonix/internal/event"
)

// An older client does not know completion_uncertain and ignores the additive
// outcome field. Keeping the bounded error text on TurnDone makes that client
// stop the run and show a generic recoverable error instead of treating the
// incomplete turn as success.
func TestCompletionUncertainSafelyDegradesForLegacyClient(t *testing.T) {
	wire := ToWire(event.Event{
		Kind:    event.TurnDone,
		Outcome: event.TurnOutcomeCompletionUncertain,
		Err:     errors.New("completion could not be confirmed; completed work was kept"),
	})
	raw, err := json.Marshal(wire)
	if err != nil {
		t.Fatal(err)
	}
	var legacy struct {
		Kind string `json:"kind"`
		Err  string `json:"err,omitempty"`
	}
	if err := json.Unmarshal(raw, &legacy); err != nil {
		t.Fatal(err)
	}
	if legacy.Kind != "turn_done" || legacy.Err == "" {
		t.Fatalf("legacy TurnDone = %+v, want terminal event with non-empty error", legacy)
	}
}
