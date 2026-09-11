package evidence

import (
	"encoding/json"
	"fmt"
	"testing"
)

func grepReceipt(pattern, path string) Receipt {
	args := json.RawMessage(fmt.Sprintf(`{"pattern":%q,"path":%q}`, pattern, path))
	r := ReceiptFromToolCall("grep", args, true, true)
	r.OutputBytes = 64
	return r
}

func windowReceipt(path string, offset int) Receipt {
	args := json.RawMessage(fmt.Sprintf(`{"path":%q,"offset":%d,"limit":300}`, path, offset))
	r := ReceiptFromToolCall("read_file", args, true, true)
	r.OutputBytes = 64
	return r
}

// Read novelty used to be keyed on the path alone, so the two most common
// investigation moves — grepping one package for a second symbol, paging
// through a long file — scored as repeats from the second round on, and a turn
// doing exactly that was told it had produced no new evidence.
func TestProgressTrackerScoresTheQuestionNotJustThePath(t *testing.T) {
	tr := NewProgressTracker()

	if got := tr.ScoreRound([]Receipt{grepReceipt("Compose", "internal/agent")}); got != gainNewRead {
		t.Fatalf("first grep = %d, want %d", got, gainNewRead)
	}
	if got := tr.ScoreRound([]Receipt{grepReceipt("Receipt", "internal/agent")}); got != gainNewRead {
		t.Fatalf("new pattern over a read path = %d, want %d", got, gainNewRead)
	}
	if got := tr.ScoreRound([]Receipt{grepReceipt("Receipt", "internal/agent")}); got != 0 {
		t.Fatalf("identical grep = %d, want 0", got)
	}

	if got := tr.ScoreRound([]Receipt{windowReceipt("agent.go", 0)}); got != gainNewRead {
		t.Fatalf("first window = %d, want %d", got, gainNewRead)
	}
	if got := tr.ScoreRound([]Receipt{windowReceipt("agent.go", 300)}); got != gainNewRead {
		t.Fatalf("next window of the same file = %d, want %d", got, gainNewRead)
	}
	if got := tr.ScoreRound([]Receipt{windowReceipt("agent.go", 300)}); got != 0 {
		t.Fatalf("identical window = %d, want 0", got)
	}

	// A first read of a new path still counts once, not twice. It needs a fresh
	// tracker: past explorationRunLimit look-only rounds the run stops counting
	// whatever it finds, which is the separate cliff this change does not touch.
	fresh := NewProgressTracker()
	first := readReceipt("new.go")
	first.OutputBytes = 64
	if got := fresh.ScoreRound([]Receipt{first}); got != gainNewRead {
		t.Fatalf("new path = %d, want %d", got, gainNewRead)
	}
}

// The shadow scorer keys novelty the same way, so the two never disagree about
// what counts as a repeat.
func TestOutcomeTrackerScoresTheQuestionNotJustThePath(t *testing.T) {
	tr := NewOutcomeTracker()

	if s := tr.ScoreRound([]Receipt{grepReceipt("Compose", "internal/agent")}); s.Exploration != 1 {
		t.Fatalf("first grep = %+v, want exploration 1", s)
	}
	if s := tr.ScoreRound([]Receipt{grepReceipt("Receipt", "internal/agent")}); s.Exploration != 1 {
		t.Fatalf("new pattern over a read path = %+v, want exploration 1", s)
	}
	if s := tr.ScoreRound([]Receipt{grepReceipt("Receipt", "internal/agent")}); s.Exploration != 0 {
		t.Fatalf("identical grep = %+v, want exploration 0", s)
	}
	if s := tr.ScoreRound([]Receipt{windowReceipt("agent.go", 0)}); s.Exploration != 1 {
		t.Fatalf("first window = %+v, want exploration 1", s)
	}
	if s := tr.ScoreRound([]Receipt{windowReceipt("agent.go", 300)}); s.Exploration != 1 {
		t.Fatalf("next window of the same file = %+v, want exploration 1", s)
	}
}
