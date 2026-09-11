package jobs

import (
	"strings"

	"reasonix/internal/evidence"
)

func mergePublishedEvidence(dst *evidence.ChildEvidenceSummary, summary evidence.ChildEvidenceSummary) {
	if dst.WorkspaceRoot == "" {
		dst.WorkspaceRoot = strings.TrimSpace(summary.WorkspaceRoot)
	}
	dst.Receipts = append(dst.Receipts, summary.Receipts...)
}
