package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

// FinishOutcome is the model-declared result of a successfully finalized turn.
// It is deliberately small and stable: the visible answer remains ordinary
// assistant text while this tool supplies only the machine-readable boundary.
type FinishOutcome string

const (
	FinishCompleted FinishOutcome = "completed"
	FinishPartial   FinishOutcome = "partial"
	FinishBlocked   FinishOutcome = "blocked"
)

// FinishTool is the host-consumed terminal tool for ordinary executor turns.
// Like submit_plan, it pairs a tool result in provider history but does not
// require a follow-up acknowledgement from the model.
type FinishTool struct{}

func (*FinishTool) finalizesTurn() {}

func NewFinishTool() *FinishTool { return &FinishTool{} }

func (*FinishTool) Name() string { return "finish" }

func (*FinishTool) Description() string {
	return "Finalize this turn after giving the user a visible final answer. Call finish exactly once, in a separate tool-call batch after all work is done. Use completed when the request is fully handled, partial when useful work was completed but some requested work remains, or blocked when progress requires external state or user input. To ask the user a question, call ask instead of finish."
}

func (*FinishTool) Schema() json.RawMessage {
	// Keep this byte sequence fixed. Registry.Add canonicalizes it once and the
	// sorted schema surface then remains identical on every later provider call.
	return json.RawMessage(`{"type":"object","properties":{"outcome":{"type":"string","enum":["completed","partial","blocked"],"description":"Machine-readable result of this turn."}},"required":["outcome"],"additionalProperties":false}`)
}

func (*FinishTool) ReadOnly() bool { return true }

func (*FinishTool) Execute(_ context.Context, args json.RawMessage) (string, error) {
	var in struct {
		Outcome FinishOutcome `json:"outcome"`
	}
	dec := json.NewDecoder(strings.NewReader(string(args)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		if err == nil {
			err = fmt.Errorf("multiple JSON values")
		}
		return "", fmt.Errorf("invalid args: %w", err)
	}
	switch in.Outcome {
	case FinishCompleted, FinishPartial, FinishBlocked:
		return "Turn finalization accepted by the host.", nil
	default:
		return "", fmt.Errorf("invalid outcome %q", in.Outcome)
	}
}

func finishOutcomeFromArgs(args string) (FinishOutcome, bool) {
	var in struct {
		Outcome FinishOutcome `json:"outcome"`
	}
	if err := json.Unmarshal([]byte(args), &in); err != nil {
		return "", false
	}
	switch in.Outcome {
	case FinishCompleted, FinishPartial, FinishBlocked:
		return in.Outcome, true
	default:
		return "", false
	}
}
