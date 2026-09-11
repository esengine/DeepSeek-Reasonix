package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/provider"
)

// seedPreviewSessions writes synthetic sessions with fresh BranchMeta
// sidecars so the cached-preview contract can be checked against the full
// decode on the same files.
func seedPreviewSessions(t *testing.T, dir string, n, turns int) {
	t.Helper()
	for i := range n {
		path := filepath.Join(dir, fmt.Sprintf("preview-session-%03d.jsonl", i))
		f, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		enc := json.NewEncoder(f)
		var msgs []provider.Message
		for turn := range turns {
			u := provider.Message{Role: provider.RoleUser, Content: fmt.Sprintf("turn %d: help me refactor the cold-start path for host %d", turn, i)}
			a := provider.Message{Role: provider.RoleAssistant, Content: "assistant answer"}
			msgs = append(msgs, u, a)
			if err := enc.Encode(u); err != nil {
				t.Fatal(err)
			}
			if err := enc.Encode(a); err != nil {
				t.Fatal(err)
			}
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
		preview, previewTurns := SessionPreviewFromMessages(msgs)
		digest, err := digestSessionMessages(msgs)
		if err != nil {
			t.Fatal(err)
		}
		meta := BranchMeta{
			Turns:         previewTurns,
			Preview:       preview,
			SchemaVersion: BranchMetaCountsVersion,
			Revision:      1,
			ContentDigest: digestString(digest),
		}
		stampSessionListingProjection(&meta)
		if err := saveBranchMeta(path, meta, false); err != nil {
			t.Fatal(err)
		}
	}
}

// TestSessionPreviewCachedMatchesDecode pins the sidecar fast path to the
// full decode: same preview, same turn count, and a stale sidecar reports
// ok=false so callers (the serve /sessions listing) fall back instead of
// trusting outdated counts.
func TestSessionPreviewCachedMatchesDecode(t *testing.T) {
	dir := t.TempDir()
	seedPreviewSessions(t, dir, 3, 4)
	for i := range 3 {
		p := filepath.Join(dir, fmt.Sprintf("preview-session-%03d.jsonl", i))
		wantPreview, wantTurns := SessionPreview(p)
		gotPreview, gotTurns, ok := SessionPreviewCached(p)
		if !ok {
			t.Fatalf("%s: sidecar not fresh", p)
		}
		if gotPreview != wantPreview || gotTurns != wantTurns {
			t.Fatalf("%s: cached=(%q,%d) want decode=(%q,%d)", p, gotPreview, gotTurns, wantPreview, wantTurns)
		}
	}

	// A stale sidecar (pre-counts schema) must report ok=false and the decode
	// fallback still sees the real content.
	stale := filepath.Join(dir, "stale.jsonl")
	if err := os.WriteFile(stale, []byte(`{"role":"user","content":"hello stale"}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := saveBranchMeta(stale, BranchMeta{SchemaVersion: 0}, false); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := SessionPreviewCached(stale); ok {
		t.Fatal("stale sidecar should not report cached")
	}
	if _, turns := SessionPreview(stale); turns != 1 {
		t.Fatalf("decode fallback sees turns=%d, want 1", turns)
	}
}

func TestSessionPreviewCachedRejectsProjectionFromPreviousTranscriptGeneration(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "advanced.jsonl")
	oldMessages := []provider.Message{{Role: provider.RoleUser, Content: "old preview"}}
	writeSessionFile(t, path, oldMessages)
	oldDigest, err := digestSessionMessages(oldMessages)
	if err != nil {
		t.Fatal(err)
	}
	meta := BranchMeta{
		Preview:       "old preview",
		Turns:         1,
		SchemaVersion: BranchMetaCountsVersion,
		Revision:      1,
		ContentDigest: digestString(oldDigest),
	}
	stampSessionListingProjection(&meta)
	if err := saveBranchMeta(path, meta, false); err != nil {
		t.Fatal(err)
	}

	newMessages := []provider.Message{
		{Role: provider.RoleUser, Content: "new preview"},
		{Role: provider.RoleAssistant, Content: "answer"},
		{Role: provider.RoleUser, Content: "follow up"},
	}
	writeSessionFile(t, path, newMessages)
	newDigest, err := digestSessionMessages(newMessages)
	if err != nil {
		t.Fatal(err)
	}
	if err := UpdateBranchMeta(path, false, func(current *BranchMeta) error {
		// Simulate a durable transcript revision followed by the warn-only
		// listing projection write failing. The old binding must remain stale.
		current.Revision = 2
		current.ContentDigest = digestString(newDigest)
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	if preview, turns, ok := SessionPreviewCached(path); ok {
		t.Fatalf("previous-generation projection reported fresh: preview=%q turns=%d", preview, turns)
	}
	if preview, turns := SessionPreview(path); preview != "new preview" || turns != 2 {
		t.Fatalf("decode fallback=(%q,%d), want (%q,2)", preview, turns, "new preview")
	}
}
