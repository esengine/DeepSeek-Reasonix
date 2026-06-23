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
