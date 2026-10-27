package control

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"reasonix/internal/runtime/agent"
	"reasonix/internal/state/store"
)

// Isolate planner-local reported spend from the earlier reservation gate.
// Persisted renewal and refusal have separate combined-owner coverage.
func goalReportedBudgetContext(t *testing.T, ctx context.Context) context.Context {
	t.Helper()
	path, scope := filepath.Join(t.TempDir(), "owner.jsonl"), "reported-planner-budget"
	dir := store.SessionBudgetDir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(map[string]any{"version": 1, "scope": scope, "limit": agent.TaskBudget{}, "started": time.Now(), "tokens": 0, "currency": "", "cost": map[string]int{}, "reserved": map[string]any{}})
	if err != nil {
		t.Fatal(err)
	}
	name := fmt.Sprintf("%x.json", sha256.Sum256([]byte(scope)))
	if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
		t.Fatal(err)
	}
	bound, err := agent.RestoreSharedTaskBudget(ctx, path, scope)
	if err != nil {
		t.Fatal(err)
	}
	return bound
}
