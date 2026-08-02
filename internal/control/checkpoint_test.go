package control

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/checkpoint"
)

// TestCheckpointListStripsComposedPrefixes verifies that rewind pickers see the
// user's real input, not the Compose-injected prefixes (<reasoning-language>,
// <memory-update>, plan marker, …). Checkpoints store the composed input
// verbatim; the read path strips the host-injected blocks.
func TestCheckpointListStripsComposedPrefixes(t *testing.T) {
	dir := t.TempDir()
	m := &checkpointManager{}
	m.rebind(dir, dir)

	composed := "<memory-update>\nA new fact\n</memory-update>\n\n" +
		"<reasoning-language>\n必须使用简体中文书写全部可见思考/推理文本\n</reasoning-language>\n\n" +
		PlanModeMarker + "\n\n真实的用户输入"
	m.begin(composed, 0)

	metas := m.list()
	if len(metas) != 1 {
		t.Fatalf("checkpoints = %+v, want 1", metas)
	}
	if got := metas[0].Prompt; got != "真实的用户输入" {
		t.Fatalf("checkpoint prompt = %q, want the user input without compose prefixes", got)
	}

	// A resumed session reloads the same composed input from disk; the read
	// path must strip it there too.
	m2 := &checkpointManager{}
	m2.rebind(dir, dir)
	metas2 := m2.list()
	if len(metas2) != 1 || metas2[0].Prompt != "真实的用户输入" {
		t.Fatalf("reloaded checkpoints = %+v, want the clean prompt", metas2)
	}
}

// TestCheckpointListStripsLegacyPersistedPrompts covers checkpoints persisted
// by older releases, which stored the composed input verbatim (injected blocks
// included). The read path must strip them so rewind pickers still show the
// user's actual prompt.
func TestCheckpointListStripsLegacyPersistedPrompts(t *testing.T) {
	dir := t.TempDir()
	legacy := checkpoint.Checkpoint{
		Turn:     0,
		Prompt:   "<reasoning-language>zh</reasoning-language>\n\nfix the bug",
		MsgIndex: 0,
	}
	b, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "turn-0.json"), b, 0o644); err != nil {
		t.Fatal(err)
	}

	m := &checkpointManager{}
	m.rebind(dir, dir)
	metas := m.list()
	if len(metas) != 1 {
		t.Fatalf("checkpoints = %+v, want 1", metas)
	}
	if got := metas[0].Prompt; got != "fix the bug" {
		t.Fatalf("legacy checkpoint prompt = %q, want the stripped user input", got)
	}
}

// TestCheckpointListLeavesPlainInputAlone guards against stripping a prompt
// that was never composed (no injected blocks to remove): the read path must
// return it unchanged.
func TestCheckpointListLeavesPlainInputAlone(t *testing.T) {
	dir := t.TempDir()
	m := &checkpointManager{}
	m.rebind(dir, dir)

	for _, prompt := range []string{"add the parser", "multi\nline input"} {
		m.begin(prompt, 0)
	}
	metas := m.list()
	if len(metas) != 2 {
		t.Fatalf("checkpoints = %+v, want 2", metas)
	}
	if metas[0].Prompt != "add the parser" || metas[1].Prompt != "multi\nline input" {
		t.Fatalf("plain prompts changed by the read path: %+v", metas)
	}
	if strings.Contains(metas[0].Prompt, "<") || strings.Contains(metas[1].Prompt, "<") {
		t.Fatalf("unexpected markup in plain prompts: %+v", metas)
	}
}
