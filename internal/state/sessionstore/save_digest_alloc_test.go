package sessionstore

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"reasonix/internal/contract/provider"
)

func digestFixture(n int) []provider.Message {
	msgs := make([]provider.Message, 0, n)
	for i := range n {
		role := provider.RoleUser
		if i%2 == 1 {
			role = provider.RoleAssistant
		}
		msgs = append(msgs, provider.Message{
			Role:      role,
			Content:   fmt.Sprintf("turn %d <b>&amp;</b>   日本語 %s", i, strings.Repeat("keep going. ", 5)),
			CreatedAt: int64(i + 1),
		})
		if i%3 == 1 {
			last := &msgs[len(msgs)-1]
			last.ToolCalls = []provider.ToolCall{{ID: fmt.Sprintf("call_%d", i), Name: "bash", Arguments: `{"command":"echo '<x>' && ls"}`, Added: i, Removed: 1}}
			last.ResponsesItems = []json.RawMessage{json.RawMessage(`{"type":"web_search_call","q":"a<b"}`)}
			last.DecisionReceipt = &provider.DecisionReceipt{}
			last.ToolFailure = &provider.ToolFailure{}
			last.ToolExecution = &provider.ToolExecution{}
		}
	}
	return msgs
}

// referenceDigest is the transcript identity as the format defines it: each
// message encoded with CreatedAt cleared, one per line, hashed in order.
func referenceDigest(msgs []provider.Message, k int) (digest [sha256.Size]byte, size int64, prefix [sha256.Size]byte) {
	h := sha256.New()
	if k == 0 {
		copy(prefix[:], h.Sum(nil))
	}
	for i, m := range msgs {
		m.CreatedAt = 0
		b, err := json.Marshal(m)
		if err != nil {
			panic(err)
		}
		h.Write(b)
		h.Write([]byte{'\n'})
		size += int64(len(b)) + 1
		if i+1 == k {
			copy(prefix[:], h.Sum(nil))
		}
	}
	copy(digest[:], h.Sum(nil))
	return digest, size, prefix
}

func TestDigestSizeAndPrefixKeepsTheTranscriptIdentity(t *testing.T) {
	msgs := digestFixture(40)
	for _, k := range []int{-1, 0, 1, 17, 40} {
		wantD, wantS, wantP := referenceDigest(msgs, k)
		gotD, gotS, gotP, err := digestSizeAndPrefix(context.Background(), msgs, k)
		if err != nil {
			t.Fatal(err)
		}
		if gotD != wantD || gotS != wantS || (k >= 0 && gotP != wantP) {
			t.Fatalf("k=%d: digest, size or prefix drifted from the format's definition (size %d, want %d)", k, gotS, wantS)
		}
	}
}

// A save digests the whole transcript each time, so what it allocates per
// message is paid again on every turn of a long session.
func TestDigestSizeAndPrefixDoesNotAllocatePerMessage(t *testing.T) {
	msgs := digestFixture(2000)
	allocs := testing.AllocsPerRun(5, func() {
		if _, _, _, err := digestSizeAndPrefix(context.Background(), msgs, 1000); err != nil {
			t.Fatal(err)
		}
	})
	if perMessage := allocs / float64(len(msgs)); perMessage > 0.5 {
		t.Fatalf("%.2f allocations per message (%.0f in all); want the digest to reuse one buffer", perMessage, allocs)
	}
}

// The index is rewritten whole on every save and read back by tools, never by
// eye, so indentation is only bytes the disk and the parser both pay for.
func TestSessionDisplayIndexIsWrittenCompact(t *testing.T) {
	msgs := digestFixture(50)
	digest, _, _ := referenceDigest(msgs, -1)
	idx := BuildSessionDisplayIndex(msgs, 1, true, digest)
	path := t.TempDir() + "/s.display.json"
	if err := WriteSessionDisplayIndex(path, idx); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(raw), "\n"); n != 1 {
		t.Fatalf("index file has %d line breaks; want only the trailing one", n)
	}
	back, err := LoadSessionDisplayIndex(path)
	if err != nil || back.MessageCount != len(msgs) || len(back.Entries) != len(idx.Entries) {
		t.Fatalf("round trip: %+v %v", back, err)
	}
}
