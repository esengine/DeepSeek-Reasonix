package main

import (
	"encoding/json"
	"strings"
	"testing"

	"reasonix/internal/event"
)

func TestWireEventTabPreservesSharedRetryingFields(t *testing.T) {
	w := toWireTab(event.Event{Kind: event.Retrying, RetryAttempt: 3, RetryMax: 10}, "tab-1")
	b, err := json.Marshal(w)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(b)
	for _, want := range []string{`"kind":"retrying"`, `"retryAttempt":3`, `"retryMax":10`, `"tabId":"tab-1"`} {
		if !strings.Contains(s, want) {
			t.Fatalf("tab retrying JSON = %s, want it to contain %s", s, want)
		}
	}
}

func TestWireEventTabPreservesTurnDoneCheckpointZero(t *testing.T) {
	turn := 0
	b, err := json.Marshal(toWireTabWithSubmission(event.Event{Kind: event.TurnDone, CheckpointTurn: &turn}, "tab-1", "runtime-1", "submission-1", 0))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, want := range []string{`"kind":"turn_done"`, `"checkpointTurn":0`, `"tabId":"tab-1"`, `"runtimeEpoch":"runtime-1"`, `"submissionId":"submission-1"`} {
		if !strings.Contains(string(b), want) {
			t.Fatalf("tab TurnDone JSON = %s, want it to contain %s", b, want)
		}
	}
}

func TestWireEventTabCarriesAuthoritativeTurnStart(t *testing.T) {
	const startedAt = int64(1_723_456_789_000)
	b, err := json.Marshal(toWireTabWithSubmission(event.Event{Kind: event.TurnStarted}, "tab-1", "runtime-1", "submission-1", startedAt))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, want := range []string{`"kind":"turn_started"`, `"turnStartedAt":1723456789000`, `"tabId":"tab-1"`, `"submissionId":"submission-1"`} {
		if !strings.Contains(string(b), want) {
			t.Fatalf("tab TurnStarted JSON = %s, want it to contain %s", b, want)
		}
	}
}
