package control

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/contract/config"
	"reasonix/internal/ext/plugin"
)

func TestRemovingAnMCPServerForgetsItsApprovedToolDefinitions(t *testing.T) {
	isolateControlConfigHome(t)
	workspace := t.TempDir()
	stateDir := plugin.MCPStateDir(config.ReasonixHomeDir(), workspace, "gone")
	sum := sha256.Sum256([]byte("gone"))
	record := filepath.Join(filepath.Dir(stateDir), ".tool-pins", hex.EncodeToString(sum[:])+".json")
	if err := os.MkdirAll(filepath.Dir(record), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(record, []byte(`{"version":1,"tools":{}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if state := reconcileRemovedMCPState(workspace, "gone"); state.cleanupErr != nil {
		t.Fatal(state.cleanupErr)
	}
	if _, err := os.Stat(record); !os.IsNotExist(err) {
		t.Fatalf("approval record survived the removal: %v", err)
	}
}
