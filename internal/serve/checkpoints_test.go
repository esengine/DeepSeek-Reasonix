package serve

import (
	"slices"
	"testing"
	"time"

	"reasonix/internal/checkpoint"
)

func TestServeCheckpointMetasExposeRewindCapabilities(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	raw := []checkpoint.Meta{
		{Turn: 1, Prompt: "first", Time: now, CanUndoFiles: true},
		{Turn: 2, Prompt: "second", Paths: []string{"z.go", "a.go"}, Time: now.Add(time.Second), CanUndoFiles: true},
	}
	boundaries := map[int]bool{1: true, 2: false}

	got := serveCheckpointMetas(raw, func(turn int) bool { return boundaries[turn] })
	if len(got) != 2 {
		t.Fatalf("checkpoint count = %d, want 2", len(got))
	}
	if !got[0].CanCode || !got[0].CanConversation {
		t.Fatalf("first capabilities = code:%v conversation:%v, want true/true", got[0].CanCode, got[0].CanConversation)
	}
	if got[1].CanConversation {
		t.Fatal("checkpoint without a message boundary must not advertise conversation rewind")
	}
	if got[0].FileCount != 2 || got[0].TurnFileCount != 0 || !slices.Equal(got[0].Files, []string{"a.go", "z.go"}) {
		t.Fatalf("first cumulative files = %#v count=%d turnCount=%d", got[0].Files, got[0].FileCount, got[0].TurnFileCount)
	}
	if got[0].Time != now.UnixMilli() {
		t.Fatalf("first time = %d, want %d", got[0].Time, now.UnixMilli())
	}
}
