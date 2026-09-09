package agent

import (
	"context"
	"fmt"
	"testing"
	"time"

	"reasonix/internal/event"
	"reasonix/internal/provider"
)

// Replays the completed-full-read/dedup collision observed with a real model.
// No network or credentials are used by this regression reproducer.
func TestReadEvidenceLiveFindingCompletedFullReread(t *testing.T) {
	path := makeShortPagedReadFixture(t, "fixture.txt", 2105, 2051, "ALPHA_MARKER")
	var turns [][]provider.Chunk
	turns = append(turns, []provider.Chunk{toolCallChunk("initial", "read_file", fmt.Sprintf(`{"path":%q,"intent":"full"}`, path)), {Type: provider.ChunkDone}})
	turns = append(turns, []provider.Chunk{toolCallChunk("tail", "read_file", fmt.Sprintf(`{"path":%q,"offset":2000,"limit":2000}`, path)), {Type: provider.ChunkDone}})
	for i := range 5 {
		turns = append(turns, []provider.Chunk{toolCallChunk(fmt.Sprintf("recheck-%d", i), "read_file", fmt.Sprintf(`{"path":%q,"intent":"full"}`, path)), {Type: provider.ChunkDone}})
	}
	turns = append(turns, textTurn("The unchanged file has already been read completely."))
	a := newIncompleteReadTestAgent(&scriptedProvider{turns: turns}, incompleteReadBuiltin(t), NewSession("sys"), event.Discard)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := a.Run(ctx, "Read the entire file, then verify the read is complete.")
	if err != nil {
		t.Fatalf("an unchanged, fully covered file became incomplete after deduplicated re-read: %T; recheck result=%s", err, toolResultByID(a.Session(), "recheck-0"))
	}
}
