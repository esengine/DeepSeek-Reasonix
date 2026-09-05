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

// ReviewReportGrant is what the host bound one report tool to before its
// execution began: the kind that execution was contracted to deliver, what its
// report is permitted to prove, and which execution it is. All three are host
// state. None of them is reachable from the payload, which is the point — the
// model writes the report's content and never its standing.
type ReviewReportGrant struct {
	// Delivery is the kind this execution owes. A submission naming any other
	// kind is refused rather than recorded under the one it named.
	Delivery evidence.ReviewKind
	// Authority is what a report from this execution may close.
	Authority evidence.ReviewAuthority
	// Execution names the run the grant was issued to, empty where the host
	// started this worker without one. Empty stays empty; it is never guessed.
	Execution string
}

// ReviewReportTool is visible only inside review/security_review subagent
// registries. It submits the structured result the parent's review gate reads.
// It is never registered on the parent agent tool surface.
type ReviewReportTool struct{ grant ReviewReportGrant }

func NewReviewReportTool(grant ReviewReportGrant) *ReviewReportTool {
	return &ReviewReportTool{grant: grant}
}

// Grant is what the host bound this tool to, for the receipt stamp.
func (t *ReviewReportTool) Grant() ReviewReportGrant { return t.grant }

func (*ReviewReportTool) Name() string { return "review_report" }

func (*ReviewReportTool) Description() string {
	return "Submit a structured review result for the parent's gate. Call once when the review is complete; once a report is accepted this run is finished reporting. kind must be the kind this run was assigned — the host already knows which, and a mismatch is refused, not recorded. verdict is pass, warn, or block: pass asserts no problem is there and carries the same burden as any negative claim, warn is what you owe when you could not establish that — including a change set you did not finish reading — and block stops delivery until the turn changes something. reviewed_paths lists only files you actually read this run; the host checks each one against its own read receipts. findings list severity/summary/path/line."
}

func (*ReviewReportTool) ReadOnly() bool { return true }

func (*ReviewReportTool) Schema() json.RawMessage {
	// Fixed schema — stable for review subagents only.
	return json.RawMessage(`{
		"type":"object",
		"properties":{
			"kind":{"type":"string","description":"Must equal the kind this run was assigned (review | security). It confirms the assignment; it does not choose one."},
			"verdict":{"type":"string","description":"pass = no problem found and you can say what you read that would have surfaced one; warn = you could not establish that, or did not finish the change set; block = do not ship"},
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

func (t *ReviewReportTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	report, err := evidence.ParseReviewReport(args)
	if err != nil {
		return "", err
	}
	// The kind asserts an assignment the host made, so it is checked against
	// that assignment and never allowed to replace it — here, before a receipt
	// exists, rather than downstream where a gate has to unpick it.
	if report.Kind != t.grant.Delivery {
		return "", fmt.Errorf("review_report rejected: this run was assigned kind=%q and submitted kind=%q; a run cannot choose which review it counts as — submit your findings under kind=%q, or hand the other review to the worker that owes it",
			string(t.grant.Delivery), string(report.Kind), string(t.grant.Delivery))
	}
	// reviewed_paths is a host-verified claim, not a model attestation: without
	// a read/diff receipt behind each path, a subagent could "cover" files it
	// never opened and the parent delivery gate would trust it.
	led, ok := evidence.FromContext(ctx)
	if !ok {
		return "", fmt.Errorf("review_report requires the host evidence ledger; submit it from inside a review subagent run")
	}
	// Accepted exactly once, not called exactly once: a refusal records nothing,
	// so a run that got the schema or the kind wrong can still correct itself
	// while one that already reported cannot file a second verdict.
	if led.AcceptedReviewReports() > 0 {
		return "", fmt.Errorf("review_report rejected: this run already submitted an accepted report, and a run reports once; if the accepted verdict was wrong, say so in your final answer rather than filing a second one")
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
		return "", fmt.Errorf("review_report rejected: no host-observed read evidence for: %s — open these files, or diff them with whatever version control this workspace has, before reporting them as reviewed", strings.Join(unread, ", "))
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

var _ tool.Tool = (*ReviewReportTool)(nil)

// AttachReviewReportTool adds review_report to a subagent registry used by
// review / security_review skills only, bound to what the host granted this
// execution. The grant is an argument rather than something the tool reads at
// call time so that mounting the instrument and issuing the permission are one
// act: there is no reachable state in which the tool is present unbound.
func AttachReviewReportTool(reg *tool.Registry, grant ReviewReportGrant) {
	if reg == nil {
		return
	}
	reg.Add(NewReviewReportTool(grant))
}

// HasSuccessfulReviewReport reports whether this agent's evidence ledger holds
// a successful review_report of the given kind.
func (a *Agent) HasSuccessfulReviewReport(kind evidence.ReviewKind) bool {
	if a == nil || a.task.ledger == nil {
		return false
	}
	return a.task.ledger.HasSuccessfulReviewReportOfKind(kind)
}

// IssueReviewGrant issues one run's permission. An execution the host cannot
// name is granted nothing: the report is still owed, and a block from it still
// reaches the parent, but a proof that cannot be joined back to the run that
// produced it is not one. Never invent a name to get past this.
func IssueReviewGrant(delivery evidence.ReviewKind, authority evidence.ReviewAuthority, execution string) ReviewReportGrant {
	if strings.TrimSpace(execution) == "" {
		authority = evidence.ReviewAuthority{}
	}
	return ReviewReportGrant{Delivery: delivery, Authority: authority, Execution: execution}
}

// reviewGrantFor issues the grant for one delegated run. Both halves come from
// the worker's own declaration, so no entry point can widen either by asking
// differently.
func reviewGrantFor(worker WorkerSpec, execution string) ReviewReportGrant {
	return IssueReviewGrant(worker.ReviewReport, worker.ReviewAuthority, execution)
}

// attachReviewReport mounts the report tool for a worker that owes a verdict.
// A worker owing none never sees it, which is also why an ordinary task cannot
// file one: the instrument is absent, not merely refused.
func attachReviewReport(reg *tool.Registry, grant ReviewReportGrant) {
	if grant.Delivery == "" {
		return
	}
	AttachReviewReportTool(reg, grant)
}
