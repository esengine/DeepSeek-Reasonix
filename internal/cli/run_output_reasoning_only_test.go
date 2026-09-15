package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"reasonix/internal/event"
)

// The shape a reasoning-only turn takes on the Message event: Reasoning holds
// the whole turn, Text is empty (internal/event/event.go, Message).
//
// Observed against a local provider, where the turn's only output was a
// malformed tool call the model wrote as prose instead of emitting as a tool
// call. Headlessly that reached the caller as `result: ""` with
// `subtype: "success"` and exit 0.
var errRunFailedFixture = errors.New("provider failed")

const reasoningOnlyTurn = `<|tool_call>call:bash{command:<|"|>mkdir -p docker<|"|>}<tool_call|>`

func TestTextOutputFallsBackToReasoningWhenNoVisibleText(t *testing.T) {
	var out bytes.Buffer
	sink := newRunOutputSink(&out, runOutputText)
	sink.Emit(event.Event{Kind: event.Reasoning, Text: reasoningOnlyTurn})
	sink.Emit(event.Event{Kind: event.Message, Reasoning: reasoningOnlyTurn})
	if err := sink.Finalize("session", time.Now(), nil); err != nil {
		t.Fatal(err)
	}
	if got := out.String(); got != reasoningOnlyTurn+"\n" {
		t.Fatalf("text output = %q, want the reasoning-only turn rather than nothing", got)
	}
}

func TestJSONResultCarriesReasoningAndSaysSo(t *testing.T) {
	var out bytes.Buffer
	sink := newRunOutputSink(&out, runOutputJSON)
	sink.Emit(event.Event{Kind: event.Message, Reasoning: reasoningOnlyTurn})
	if err := sink.Finalize("session", time.Now(), nil); err != nil {
		t.Fatal(err)
	}
	var got runResult
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Result != reasoningOnlyTurn {
		t.Fatalf("result = %q, want the reasoning-only turn", got.Result)
	}
	if !got.ResultFromReasoning {
		t.Fatal("result_from_reasoning = false; a caller cannot tell this from visible text the model never emitted")
	}
}

// Visible text still wins, and the flag stays off, so nothing about an ordinary
// answer changes - including the case where a thinking model emits both.
func TestVisibleTextIsUnchangedByTheFallback(t *testing.T) {
	for _, tc := range []struct {
		name  string
		event event.Event
	}{
		{"text only", event.Event{Kind: event.Message, Text: "final answer"}},
		{"text beside reasoning", event.Event{Kind: event.Message, Text: "final answer", Reasoning: "thinking"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			sink := newRunOutputSink(&out, runOutputJSON)
			sink.Emit(tc.event)
			if err := sink.Finalize("session", time.Now(), nil); err != nil {
				t.Fatal(err)
			}
			var got runResult
			if err := json.Unmarshal(out.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if got.Result != "final answer" || got.ResultFromReasoning {
				t.Fatalf("result = %q, from_reasoning = %v", got.Result, got.ResultFromReasoning)
			}
			if bytes.Contains(out.Bytes(), []byte("result_from_reasoning")) {
				t.Fatalf("omitempty dropped: %s", out.Bytes())
			}
		})
	}
}

// A later turn that answers normally must clear the flag the earlier one set,
// or a multi-turn run reports the wrong provenance for its final answer.
func TestLaterVisibleTurnClearsTheReasoningProvenance(t *testing.T) {
	var out bytes.Buffer
	sink := newRunOutputSink(&out, runOutputJSON)
	sink.Emit(event.Event{Kind: event.Message, Reasoning: reasoningOnlyTurn})
	sink.Emit(event.Event{Kind: event.Message, Text: "final answer"})
	if err := sink.Finalize("session", time.Now(), nil); err != nil {
		t.Fatal(err)
	}
	var got runResult
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Result != "final answer" || got.ResultFromReasoning {
		t.Fatalf("result = %q, from_reasoning = %v", got.Result, got.ResultFromReasoning)
	}
}

// An error result is the run's own message, never the model's reasoning.
func TestErrorResultIsNotMarkedAsReasoning(t *testing.T) {
	var out bytes.Buffer
	sink := newRunOutputSink(&out, runOutputJSON)
	if err := sink.Finalize("session", time.Now(), errRunFailedFixture); err != nil {
		t.Fatal(err)
	}
	var got runResult
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Result != errRunFailedFixture.Error() || got.ResultFromReasoning {
		t.Fatalf("result = %q, from_reasoning = %v", got.Result, got.ResultFromReasoning)
	}
}
