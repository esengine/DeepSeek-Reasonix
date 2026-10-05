package usecap

import (
	"encoding/json"
	"strings"
	"testing"
)

// The fleet call that motivated this: "items" was rejected without the error
// naming "tasks", and finding that one name cost an inspect and two doc calls.
func TestContractHintNamesTheParameterTheCallShouldHaveUsed(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","properties":{"tasks":{},"max_parallel":{}},"required":["tasks"]}`)
	got := contractHint(schema, json.RawMessage(`{"items":[]}`))
	for _, want := range []string{`"items" is not a parameter`, `requires "tasks"`, `accepts "max_parallel", "tasks"`} {
		if !strings.Contains(got, want) {
			t.Errorf("hint = %q, want it to contain %q", got, want)
		}
	}
}

// A hint on a failure it cannot explain is noise: the capability may have
// failed for its own reasons while the arguments fit the schema exactly.
func TestContractHintStaysSilentWhenTheArgumentsFit(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","properties":{"tasks":{}},"required":["tasks"]}`)
	if got := contractHint(schema, json.RawMessage(`{"tasks":[]}`)); got != "" {
		t.Errorf("arguments that fit produced hint %q, want silence", got)
	}
	// Absent arguments do break a contract that requires one, so that case is
	// deliberately not silent.
	if got := contractHint(schema, json.RawMessage(``)); !strings.Contains(got, `requires "tasks"`) {
		t.Errorf("missing required parameter went unreported: %q", got)
	}
	if got := contractHint(json.RawMessage(``), json.RawMessage(`{"items":[]}`)); got != "" {
		t.Errorf("no schema must produce no hint, got %q", got)
	}
	if got := contractHint(json.RawMessage(`{"type":"object"}`), json.RawMessage(`{"items":[]}`)); got != "" {
		t.Errorf("a schema without properties must produce no hint, got %q", got)
	}
}

// An item that omits what its own schema requires is answered at the item's
// level too, so the fix does not read as a missing top-level parameter. The
// fixture is inline because a fleet item requires prompt or adopt_ref — an
// alternation no single `required` entry states, so fleet declares neither and
// refuses the pair at the host (validateFleetItemShape).
func TestContractHintNamesWhatAnItemRequires(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","properties":{"tasks":{"type":"array","items":{"type":"object","properties":{"prompt":{},"description":{}},"required":["prompt"]}}},"required":["tasks"]}`)
	got := contractHint(schema, json.RawMessage(`{"tasks":[{"description":"fix billing"}]}`))
	if !strings.Contains(got, "`tasks`"+` item requires "prompt"`) {
		t.Errorf("hint = %q, want the item's own required field named", got)
	}
}
