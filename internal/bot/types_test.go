package bot

import (
	"slices"
	"testing"
)

func TestSendResultMergeKeepsEveryDeliveredMessageID(t *testing.T) {
	var got SendResult
	got.Merge(SendResult{MessageID: "text"})
	got.Merge(SendResult{MessageID: "media-2", MessageIDs: []string{"media-1", "media-2"}})
	got.Merge(SendResult{MessageID: "media-2"})

	want := []string{"text", "media-1", "media-2"}
	if ids := got.DeliveredMessageIDs(); !slices.Equal(ids, want) {
		t.Fatalf("message IDs = %v, want %v", ids, want)
	}
	if got.MessageID != "media-2" {
		t.Fatalf("compatibility message ID = %q, want media-2", got.MessageID)
	}
}
