package agent

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/tool"
	"reasonix/internal/safety/evidence"
	"reasonix/internal/safety/sandbox"
	"reasonix/internal/state/sessionstore"
)

func TestBaselineCannotExecuteCandidateWithoutRequiredIntegrity(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "escaped")
	fixture := baselineWorkspace(t, "package spike\nimport \"os\"\nfunc init() { _ = os.WriteFile("+strconv.Quote(marker)+", []byte(\"escaped\"), 0600) }\nfunc Evict(n int) int { return n-1 }\n", assertsNothing)
	a := New(nil, tool.NewRegistry(), sessionstore.NewSession(""), Options{
		WriteWorkspaceRoot: fixture.writeWorkspaceRoot, ArchiveDir: fixture.archiveDir,
		ProcessSandbox: sandbox.Spec{Mode: "enforce", RequiredIntegrity: "unavailable_backend"},
	}, event.Discard)
	a.task.baselineCriteria = fixture.task.baselineCriteria
	a.evaluateBaselineCriteriaOnce(t.Context())
	facts := a.baselineFacts()
	if len(facts) != 1 || facts[0].Evaluation == nil || facts[0].Evaluation.Result != evidence.CriterionUnavailable {
		t.Fatalf("unconfined result was accepted: %+v", facts)
	}
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		t.Fatalf("candidate code ran on host: %v", err)
	}
	if len(evidence.BaselineTestObligations(facts, a.mutationEpoch())) != 1 {
		t.Fatal("unavailable verification settled its obligation")
	}
}
