package main

import (
	"strings"
	"testing"
)

// Automatic snip projections are gone. SnipStaleToolResults remains as a no-op
// so older harness flags and call sites do not panic or rewrite history.
func TestSnipIsNoOp(t *testing.T) {
	dir, cleanup, err := tempDir()
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()

	rec := &callRecorder{}
	const window = 64_000
	h := newHarness(dir, &scriptedProvider{rec: rec, reply: syntheticDigest, window: window}, window, rec, arms{snip: true})
	for gen := range 3 {
		growSession(h.sess, gen, probeSuite())
	}
	before := renderAll(h.sess.Snapshot())
	if !strings.Contains(before, "beta-7d21") {
		t.Fatal("planted fact missing before the snip call")
	}
	st, err := h.agentA.SnipStaleToolResults()
	if err != nil {
		t.Fatal(err)
	}
	if st.Results != 0 || st.SavedChars != 0 {
		t.Fatalf("snip no-op rewrote history: results=%d chars=%d", st.Results, st.SavedChars)
	}
	after := renderAll(h.sess.Snapshot())
	if after != before {
		t.Fatal("canonical transcript changed after SnipStaleToolResults")
	}
	if !strings.Contains(after, "beta-7d21") {
		t.Error("planted fact missing after snip no-op")
	}
}
