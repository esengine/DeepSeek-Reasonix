package jobs

import (
	"testing"

	"reasonix/internal/evidence"
)

func TestTaskMutationEvidenceUsesWorkspaceRelativeRisk(t *testing.T) {
	summary := evidence.ChildEvidenceSummary{
		WorkspaceRoot: "/workspace/toolbox",
		Receipts: []evidence.Receipt{{
			ToolName: "edit_file",
			Success:  true,
			Write:    true,
			Mutation: true,
			Paths:    []string{"/workspace/toolbox/internal/agent/worker.go"},
		}},
	}
	meta := mutationEvidenceForArtifact(summary)
	if meta == nil || meta.Risk != string(evidence.RiskMedium) {
		t.Fatalf("ordinary workspace mutation evidence = %+v, want medium risk", meta)
	}

	summary.Receipts[0].Paths = []string{"/workspace/toolbox/internal/provider/openai/responses/client.go"}
	meta = mutationEvidenceForArtifact(summary)
	if meta == nil || meta.Risk != string(evidence.RiskHigh) {
		t.Fatalf("sensitive workspace mutation evidence = %+v, want high risk", meta)
	}
}
