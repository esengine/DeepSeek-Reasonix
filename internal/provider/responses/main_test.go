package responses

import (
	"os"
	"testing"
)

// TestMain isolates the whole package's knowledge cache to a temp dir so
// tests never touch (or wipe) the real user cache
// (~/.cache/reasonix/websearch). Side-effect fix for the 100-round hammer
// test which previously deleted real cached entries.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "responses-test-cache-*")
	if err != nil {
		panic(err)
	}
	knowledgeDirOverride = dir
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

// mustKnowledgeDir exposes the effective (override) cache root for tests.
func mustKnowledgeDir(t *testing.T) string {
	t.Helper()
	d, err := knowledgeDir()
	if err != nil {
		t.Fatalf("knowledgeDir: %v", err)
	}
	return d
}
