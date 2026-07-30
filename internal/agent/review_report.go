package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"reasonix/internal/evidence"
	"reasonix/internal/tool"
)

// ReviewReportTool is visible only inside review/security_review subagent
// registries. It submits a structured review result the host uses for
// Delivery risk gates. It is never registered on the parent agent tool surface.
type ReviewReportTool struct{}

func NewReviewReportTool() *ReviewReportTool { return &ReviewReportTool{} }

func (*ReviewReportTool) Name() string { return "review_report" }

func (*ReviewReportTool) Description() string {
	return "Submit a structured review result for the parent delivery gate. Call once when the review is complete. kind is review or security; verdict is pass, warn, or block; reviewed_paths must cover the production paths you inspected; findings list severity/summary/path/line."
}

func (*ReviewReportTool) ReadOnly() bool { return true }

func (*ReviewReportTool) Schema() json.RawMessage {
	// Fixed schema — stable for review subagents only.
	return json.RawMessage(`{
		"type":"object",
		"properties":{
			"kind":{"type":"string","description":"review | security"},
			"verdict":{"type":"string","description":"pass | warn | block"},
			"reviewed_paths":{"type":"array","items":{"type":"string"},"description":"Production paths covered by this review"},
			"findings":{"type":"array","items":{"type":"object","properties":{
				"severity":{"type":"string"},
				"summary":{"type":"string"},
				"path":{"type":"string"},
				"line":{"type":"integer"}
			},"required":["severity","summary"]}}
		},
		"required":["kind","verdict","reviewed_paths"]
	}`)
}

func (*ReviewReportTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	report, err := evidence.ParseReviewReport(args)
	if err != nil {
		return "", err
	}
	// reviewed_paths is a host-verified claim, not a model attestation: every
	// path must be backed by a successful read/diff receipt in this subagent's
	// own evidence ledger. Without that check a subagent could "cover" files
	// it never opened and the parent delivery gate would trust it.
	led, ok := evidence.FromContext(ctx)
	if !ok {
		return "", fmt.Errorf("review_report requires the host evidence ledger; submit it from inside a review subagent run")
	}
	var unread []string
	for _, p := range report.ReviewedPaths {
		if led.HasReadEvidenceForPath(p) {
			continue
		}
		// Slash-canonical display keeps the message (and tests) identical
		// across OSes even though ParseReviewReport normalized with the
		// platform separator.
		unread = append(unread, filepath.ToSlash(p))
	}
	if len(unread) > 0 {
		return "", fmt.Errorf("review_report rejected: no host-observed read evidence for: %s — read these files (or run git diff on them) before reporting them as reviewed", strings.Join(unread, ", "))
	}
	// Evidence is recorded by the agent host from the tool call args; this
	// result is a human-readable confirmation for the subagent transcript.
	msg := fmt.Sprintf("review_report accepted: kind=%s verdict=%s paths=%d findings=%d",
		report.Kind, report.Verdict, len(report.ReviewedPaths), len(report.Findings))
	if report.HasBlockingFinding() {
		msg += " (blocking — parent delivery will require fixes and re-review)"
	}
	return msg, nil
}

// ReviewReportKindForSkill maps a review-capable skill name to the report kind
// its subagent must submit before finishing; empty means no requirement.
func ReviewReportKindForSkill(name string) evidence.ReviewKind {
	switch name {
	case "review":
		return evidence.ReviewKindReview
	case "security-review", "security_review":
		return evidence.ReviewKindSecurity
	}
	return ""
}

var _ tool.Tool = (*ReviewReportTool)(nil)

// AttachReviewReportTool adds review_report to a subagent registry used by
// review / security_review skills only.
func AttachReviewReportTool(reg *tool.Registry) {
	if reg == nil {
		return
	}
	reg.Add(NewReviewReportTool())
}

// HasSuccessfulReviewReport reports whether this agent's evidence ledger holds
// a successful review_report of the given kind.
func (a *Agent) HasSuccessfulReviewReport(kind evidence.ReviewKind) bool {
	if a == nil || a.evidence == nil {
		return false
	}
	return a.evidence.HasSuccessfulReviewReportOfKind(kind)
}
