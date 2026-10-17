package tui

import (
	"encoding/json"
	"strings"
	"testing"

	"reasonix/internal/base/i18n"
)

// The mirror decodes the kernel's GET /inbox body, so the rendered line is
// only as good as the field name: the body below is what the handler in
// internal/frontend/serve sends for a queue whose bodies could not be read.
func TestQueueLineStaysUnknownAfterTheWireBodyIsDecoded(t *testing.T) {
	const body = `{"schemaVersion":2,"revision":0,"paused":true,"recovered":false,"unrecovered":true,"items":[],"capacity":{"items":0,"maxItems":64,"bytes":0,"maxBytes":67108864,"maxItemBytes":4194304}}`
	var snap InboxSnapshot
	if err := json.Unmarshal([]byte(body), &snap); err != nil {
		t.Fatalf("decode GET /inbox: %v", err)
	}
	if !snap.Unrecovered {
		t.Fatal("the mirror dropped the unrecovered marker the kernel sent")
	}
	got := renderQueueList(snap)
	if !strings.Contains(got, "UNKNOWN") || strings.Contains(got, "inbox empty") {
		t.Fatalf("decoded unreadable queue rendered as %q", got)
	}
}

// An unreadable queue has to read as "unknown and paused" from the /queue line
// alone: it is neither an empty queue nor a recovered one, and the bodies it
// still holds on disk are exactly what a plain "inbox empty" would hide.
func TestQueueListSaysUnknownAndPausedWhenTheQueueWasUnreadable(t *testing.T) {
	got := renderQueueList(InboxSnapshot{Revision: 4, Paused: true, Unrecovered: true})
	for _, want := range []string{"UNKNOWN", "unreadable", "paused", "inspect"} {
		if !strings.Contains(got, want) {
			t.Errorf("unreadable queue line %q does not say %q", got, want)
		}
	}
	if strings.Contains(got, "inbox empty") {
		t.Errorf("unreadable queue rendered as an empty one: %q", got)
	}
	if !strings.Contains(i18n.English.TUIQueueUnrecovered, "UNKNOWN") {
		t.Errorf("the English wording %q must name the state as unknown", i18n.English.TUIQueueUnrecovered)
	}
}

// A readable queue that is genuinely empty keeps its plain wording, and a
// recovered one keeps the count it has always shown.
func TestQueueListKeepsEmptyAndRecoveredWording(t *testing.T) {
	if got, want := renderQueueList(InboxSnapshot{Revision: 1}), "inbox empty"; got != want {
		t.Errorf("empty queue = %q, want %q", got, want)
	}
	if got, want := renderQueueList(InboxSnapshot{Revision: 1, Paused: true}), "inbox empty (paused)"; got != want {
		t.Errorf("empty paused queue = %q, want %q", got, want)
	}
	recovered := renderQueueList(InboxSnapshot{Revision: 2, Paused: true, Recovered: true, RecoveredN: 2, Items: []InboxItem{{ID: "abcdefgh", Intent: "followup", State: "uncertain", Preview: "orphan"}}})
	if !strings.Contains(recovered, "recovered=2") || strings.Contains(recovered, "UNKNOWN") {
		t.Errorf("recovered queue = %q, want its recovered count and no unknown wording", recovered)
	}
}
