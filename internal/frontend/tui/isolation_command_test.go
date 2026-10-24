package tui

import (
	"strings"
	"testing"
)

func TestIsolatedManagementCommandCannotBecomeModelInputOrQueuedWork(t *testing.T) {
	m, kernel := queueModel(t, map[string]any{
		"POST /isolated-results/command": map[string]string{"text": "candidate retained; explicit preview required"},
	})
	m.tr.Running = true
	queueSend(m, "/isolated discard iso_12345678")
	if got := queueLastNotice(m); got != "candidate retained; explicit preview required" {
		t.Fatal(got)
	}
	if calls := kernel.seen(); !strings.Contains(calls, `POST /isolated-results/command {"args":"discard iso_12345678"}`) || strings.Contains(calls, "POST /submit") || strings.Contains(calls, "/inbox/") {
		t.Fatalf("management input escaped to a model turn: %s", calls)
	}
}
