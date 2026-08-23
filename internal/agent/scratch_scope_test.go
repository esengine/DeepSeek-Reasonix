package agent

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"reasonix/internal/checkpoint"
	"reasonix/internal/event"
	"reasonix/internal/evidence"
	"reasonix/internal/provider"
	"reasonix/internal/sessiontemp"
	"reasonix/internal/tool"
)

func TestScratchScriptExecutionKeepsCheckpointCoverageFailClosed(t *testing.T) {
	workspace := t.TempDir()
	scratch := t.TempDir()
	script := filepath.Join(scratch, "probe.py")
	store := checkpoint.New("", workspace)
	store.Begin(1, "probe", 0)
	observer := checkpoint.NewMutationObserver(checkpoint.ObserverOptions{Store: store})
	a := New(nil, tool.NewRegistry(), NewSession(""), Options{
		WriteWorkspaceRoot: workspace,
		MutationObserver:   observer,
	}, event.Discard)
	args, err := json.Marshal(map[string]string{"command": "python " + script})
	if err != nil {
		t.Fatal(err)
	}
	plan := &toolCallPlan{
		call:         provider.ToolCall{Name: "bash", Arguments: string(args)},
		tool:         fakeTool{name: "bash", readOnly: false},
		evidenceName: "bash",
		evidenceArgs: args,
	}
	a.observeBeforeMutation(t.Context(), plan)
	meta := store.List()
	if len(meta) != 1 || len(meta[0].CoverageGaps) != 1 || meta[0].CoverageGaps[0].Reason != checkpoint.GapBashSideEffect {
		t.Fatalf("coverage gaps = %+v, want bash_side_effect", meta)
	}
}

func TestRecordToolReceiptsKeepsScratchScriptExecutionFailClosed(t *testing.T) {
	workspace := t.TempDir()
	manager := sessiontemp.NewWithRoot(t.TempDir())
	manager.Retain()
	defer manager.Release()
	lease, err := manager.Acquire()
	if err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(lease.Dir(), "probe.py")
	defer lease.Release()

	a := New(nil, tool.NewRegistry(), NewSession(""), Options{
		WriteWorkspaceRoot: workspace,
		SessionTemp:        manager,
	}, event.Discard)
	for _, background := range []bool{false, true} {
		args, err := json.Marshal(map[string]any{
			"command":           "python " + script,
			"run_in_background": background,
		})
		if err != nil {
			t.Fatal(err)
		}
		plan := &toolCallPlan{
			call:         provider.ToolCall{Name: "bash", Arguments: string(args)},
			tool:         fakeTool{name: "bash", readOnly: false},
			evidenceName: "bash",
			evidenceArgs: args,
			effects:      evidence.ClassifyToolCall("bash", args, false),
		}
		a.recordToolReceipts(plan, "ok", nil, nil)
		receipts := a.task.ledger.Receipts()
		got := receipts[len(receipts)-1]
		if got.DeliveryScope == evidence.WriteScopeScratch {
			t.Fatalf("background=%v receipt = %+v, script location cannot prove its side effects", background, got)
		}
	}
}
