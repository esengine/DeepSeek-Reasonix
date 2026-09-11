package agent

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"reasonix/internal/event"
	"reasonix/internal/provider"
)

// TestImplicitReadIsABoundedPreview pins the new default: a read with no window
// and no intent is an inspect page. Content left in the file is not an
// outstanding debt, so the host neither continues nor blocks the final answer.
func TestImplicitReadIsABoundedPreview(t *testing.T) {
	const tailKey = "SOURCE_PAGE_KEY_2050"
	path := makeShortPagedReadFixture(t, "more-than-default-limit.txt", 2105, 2050, tailKey)
	read := incompleteReadBuiltin(t)
	args := fmt.Sprintf(`{"path":%q}`, path)
	first := expectedReadOutput(t, read, args)
	if !strings.Contains(first, "pass offset=2000") {
		t.Fatalf("fixture does not leave content below the first page:\n%s", first)
	}
	inner := &scriptedProvider{name: "inspect", turns: [][]provider.Chunk{
		{toolCallChunk("inspect-1", "read_file", args), {Type: provider.ChunkDone}},
		textTurn("Previewed the top of the file."),
	}}
	agent := newIncompleteReadTestAgent(inner, read, NewSession("sys"), event.Discard)
	if err := agent.Run(context.Background(), "Show me the top of that file."); err != nil {
		t.Fatalf("Run: %v", err)
	}
	observations := agent.task.ledger.TextObservations()
	firstLines := 0
	if len(observations) > 0 {
		firstLines = len(observations[0].LineHashes)
	}
	if len(observations) != 1 || firstLines != 2000 {
		t.Fatalf("observations = %d windows (first %d lines), want the single preview page", len(observations), firstLines)
	}
}
