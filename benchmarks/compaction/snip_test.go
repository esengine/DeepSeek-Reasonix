package main

import (
	"strings"
	"testing"
)

// The buried-evidence probe only measures something if its planted fact sits
// outside the head/tail window the snip geometry keeps. Guard the probe design
// against a later edit that shortens the log back into reach.
func TestBuriedFactSitsOutsideSnipGeometry(t *testing.T) {
	const (
		head = 80 // defaultReadOnlySnip.head
		tail = 12 // defaultReadOnlySnip.tail
	)
	lines := strings.Split(buriedTestLog(), "\n")
	idx := -1
	for i, line := range lines {
		if strings.Contains(line, "beta-7d21") {
			idx = i
			break
		}
	}
	if idx < 0 {
		t.Fatal("buried fact absent from the planted log")
	}
	if idx < head || idx >= len(lines)-tail {
		t.Errorf("fact at line %d of %d is inside the kept head(%d)/tail(%d); snipping would preserve it and the probe would measure nothing", idx, len(lines), head, tail)
	}
}

// A tool result the host recorded as failed outranks the snip geometry: the
// assertion detail sits where head/tail cannot reach it, so it survives only
// because the keep policy reads the execution record rather than the text.
func TestSnipKeepsRecordedFailure(t *testing.T) {
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
		t.Fatal("planted fact missing before the snip pass")
	}
	st, err := h.agentA.SnipStaleToolResults()
	if err != nil {
		t.Fatal(err)
	}
	if st.Results == 0 {
		t.Fatal("snip pass rewrote nothing")
	}
	visible, err := visibleContext(h.path, h.sess)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(renderAll(visible), "beta-7d21") {
		t.Errorf("recorded failure was snipped away (%d results, %d chars); keep policy did not protect it", st.Results, st.SavedChars)
	}
}
