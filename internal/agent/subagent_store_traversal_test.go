package agent

import (
	"path/filepath"
	"strings"
	"testing"
)

// Lineage walks build paths from session identifiers — the caller-provided
// parent id and every parent id read back from branch metadata. All of them
// must be validated as bare filename stems so a "../"-shaped id can never
// escape the session directory (#5551).
func TestSessionLineageRejectsTraversalIdentifiers(t *testing.T) {
	sessionDir := t.TempDir()
	store := NewSubagentStore(filepath.Join(sessionDir, "subagents"))

	for _, id := range []string{"../evil", "..", ".", "nested/evil", "/abs"} {
		if _, err := store.sessionAncestors(id); err == nil || !strings.Contains(err.Error(), "invalid session identifier") {
			t.Fatalf("sessionAncestors(%q) error = %v, want invalid-identifier rejection", id, err)
		}
		if _, err := store.isAncestorSession("root", id); err == nil || !strings.Contains(err.Error(), "invalid session identifier") {
			t.Fatalf("isAncestorSession(root, %q) error = %v, want invalid-identifier rejection", id, err)
		}
	}
}

func TestSessionLineageRejectsTraversalParentFromMetadata(t *testing.T) {
	sessionDir := t.TempDir()
	store := NewSubagentStore(filepath.Join(sessionDir, "subagents"))

	// A well-formed child whose stored metadata declares a traversal parent:
	// the walk must stop with a validation error instead of joining the path.
	saveTestBranchMeta(t, sessionDir, "child", "../evil")
	if _, err := store.sessionAncestors("child"); err == nil || !strings.Contains(err.Error(), "invalid session identifier") {
		t.Fatalf("sessionAncestors(child) error = %v, want invalid-identifier rejection for stored parent", err)
	}
}
