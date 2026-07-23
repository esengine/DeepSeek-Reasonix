package cli

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/recovery"
)

func TestSessionMachineRecoveryIsContentFree(t *testing.T) {
	identityKey := installMachineTestIdentity(t)
	dir := t.TempDir()
	saveMachineTestSession(t, dir, "recoverable", time.Date(2026, 7, 23, 14, 0, 0, 0, time.UTC))
	path := filepath.Join(dir, "recoverable.jsonl")
	if err := agent.MarkSessionInFlightTurn(path, 1, true); err != nil {
		t.Fatalf("mark in-flight: %v", err)
	}
	if err := recovery.SaveSnapshot(path, recovery.Snapshot{Tasks: map[string]*recovery.TaskState{
		"task": {Phase: recovery.PhaseAwaitingDecision, Pending: &recovery.PendingProposal{Tool: "bash", Subject: "PRIVATE SUBJECT", Args: []byte(`{"secret":"PRIVATE"}`)}},
	}}); err != nil {
		t.Fatalf("save recovery: %v", err)
	}

	var out bytes.Buffer
	if code := runSessionCommand([]string{"recovery", "--json", "--dir", dir}, &out); code != 0 {
		t.Fatalf("recovery exit code = %d, output = %s", code, out.String())
	}
	var response machineRecoveryList
	if err := json.Unmarshal(out.Bytes(), &response); err != nil {
		t.Fatalf("decode recovery: %v", err)
	}
	if len(response.Recoveries) != 1 {
		t.Fatalf("recoveries = %+v", response.Recoveries)
	}
	item := response.Recoveries[0]
	if item.SessionID != machineSessionIDWithKey("recoverable", identityKey) || item.State != "awaiting_decision" || item.Tasks != 1 || item.Pending != 1 || !item.InFlight {
		t.Fatalf("recovery = %+v", item)
	}
	if strings.Contains(out.String(), "PRIVATE") || strings.Contains(out.String(), dir) {
		t.Fatalf("recovery output leaked private data: %s", out.String())
	}
}
