package builtin

import (
	"os"
	"testing"

	"reasonix/internal/provider/responses"
)

// TestMain redirects the knowledge cache to a temp dir so tool tests never
// touch the real user cache (~/.cache/reasonix/websearch). The responses
// package has its own TestMain, but this process runs tool/builtin's tests —
// without the override, cleanCacheForTool wiped real cached entries
// (2026-08-03: 69 real entries deleted by TestRetrieveInfoToolSystemFetch).
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "builtin-test-cache-*")
	if err != nil {
		panic(err)
	}
	responses.SetKnowledgeDirOverride(dir)
	code := m.Run()
	_ = os.RemoveAll(dir)
	responses.SetKnowledgeDirOverride("")
	os.Exit(code)
}
