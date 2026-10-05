package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reasonix/internal/contract/hostaudit"
	"reasonix/internal/runtime/writeclaim"
	"reasonix/internal/state/checkpoint"
	"reasonix/internal/state/sessionstore"
	"reasonix/internal/state/sessiontemp"
	"slices"
	"strings"
	"time"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/planmode"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
	"reasonix/internal/safety/evidence"
	"reasonix/internal/state/memory"
	"reasonix/internal/tools/jobs"
)

// RunSubAgentWithSession continues an existing sub-agent session with prompt and
// returns the latest final assistant answer. Fresh sub-agents pass a newly-created
// session; continued sub-agents pass a loaded transcript session.
//
// Each call installs an independent session-private temporary directory Manager
// so parent, sibling, and nested sub-agents never share temporary files.
// continue_from restores conversation history only — a new run still gets a
// fresh temporary directory.
func RunSubAgentWithSession(ctx context.Context, prov provider.Provider, reg *tool.Registry, sess *sessionstore.Session, prompt string, opts Options, sink event.Sink) (answer string, err error) {
	if sess == nil {
		return "", fmt.Errorf("sub-agent session is nil")
	}
	// Isolate temporary files for this run before any tool execution.
	ctx = tool.WithoutGoalTurnRecorder(ctx)
	if opts.MemoryQueue != nil {
		ctx = memory.WithQueue(ctx, opts.MemoryQueue)
	} else {
		ctx = memory.WithoutQueue(ctx)
	}
	if opts.Jobs == nil {
		ctx = jobs.WithoutManager(ctx)
	}
	ctx, releaseTemp := withSubagentSessionTemp(ctx)
	defer releaseTemp()
	if opts.SubagentDepth > 0 {
		ctx = WithSubagentDepth(ctx, opts.SubagentDepth)
	}
	// Callers that wrap the prompt themselves (runSubSession) set
	// ClassifierTaskText before wrapping; for everyone else the prompt is
	// still pristine here, so capture it before host framing is prepended.
	if strings.TrimSpace(opts.ClassifierTaskText) == "" {
		opts.ClassifierTaskText = prompt
	}
	planWorkflow := PlanModeFromContext(ctx)
	if opts.SubagentDepth > 0 && isFreshSubagentSession(sess) {
		prompt = subagentStartContext + "\n\n" + prompt
	}
	if planWorkflow && !strings.Contains(prompt, planmode.Marker) {
		prompt = planmode.Marker + "\n\n" + prompt
	}
	if kind := opts.RequireReviewReportKind; kind != "" {
		prompt = prompt + "\n\n" + reviewReportTaskContract(kind)
	}
	// Nested reasoning stays isolated; the parent consumes only final Content.
	// Require it so a reasoning-only stop cannot fall back to older tool text.
	opts.RequireVisibleFinal = true
	sub := newDelegatedAgent(ctx, prov, reg, sess, opts, sink)
	// One defer: this returns from a salvage, a review failure, a provider
	// error and a clean answer, and per-path calls would miss one.
	defer func() { observeSubagentHandoff(sink, sub, sess, opts, answer, err) }()
	sub.SetPlanMode(planWorkflow)
	if err := sub.Run(ctx, prompt); err != nil {
		// Still merge any partial child evidence so parent gates see real writes.
		mergeChildEvidence(ctx, sub)
		if answer, ok := salvageReadinessExhaustedAnswer(sub, sess, opts, err); ok {
			return composeSubagentAnswer(ctx, answer, sub, writeclaim.SubagentWriteClaim(ctx), opts.ClassifierTaskText), nil
		}
		return "", fmt.Errorf("sub-agent: %w", err)
	}
	// Review subagents hand back a typed report the parent's gate can verify.
	// A run that finished without one is nudged on the same session, evidence
	// preserved so review_report can cite the reads it already earned.
	if kind := opts.RequireReviewReportKind; kind != "" {
		nudges := 0
		for !sub.HasSuccessfulReviewReport(kind) && nudges < maxReviewReportNudges {
			nudges++
			sub.pending.preserveEvidence = true
			if err := sub.Run(ctx, reviewReportNudgePrompt(kind)); err != nil {
				mergeChildEvidence(ctx, sub)
				// A retry that fails still keeps local parent mutations; the
				// parent turns this into Partial/Unverified rather than rolling back.
				return "", fmt.Errorf("sub-agent: %w", err)
			}
		}
		if !sub.HasSuccessfulReviewReport(kind) {
			mergeChildEvidence(ctx, sub)
			// Nudging pays at every role setting: a verdict the host can read is
			// how a block reaches the gate. Failing over its absence does not —
			// no report is no block, the state a killed run leaves anyway.
			if !opts.DeliveryProfile {
				if answer := latestAssistantAnswer(sess); answer != "" {
					return composeSubagentAnswer(ctx, reviewWithoutVerdictNote+answer, sub, writeclaim.SubagentWriteClaim(ctx), opts.ClassifierTaskText), nil
				}
			}
			dumpRef := dumpFailedSubagentSession(opts.ArchiveDir, string(kind), sess)
			// Partial path: local changes are retained; the parent readiness
			// layer treats missing review as Partial/Unverified (not rollback).
			return "", &ReviewUnavailableError{
				Kind:   string(kind),
				Nudges: nudges,
				Dump:   dumpRef,
			}
		}
	}
	mergeChildEvidence(ctx, sub)
	if answer := latestAssistantAnswer(sess); answer != "" {
		return composeSubagentAnswer(ctx, answer, sub, writeclaim.SubagentWriteClaim(ctx), opts.ClassifierTaskText), nil
	}
	return "", fmt.Errorf("sub-agent finished without producing a final answer")
}

// readOnlyAgentConstruction is the single pairing every strictly read-only
// loop shares: the permanent ReadOnlyExecution flag plus the final registry
// filter. Batch children (RunReadOnlySubAgentWithSession) and legacy call sites
// that still use NewReadOnlyAgent build through it, so a missed call site
// cannot set only half the boundary. The interactive two-model planner uses
// NewPlannerAgent instead (PlannerMCPExecution).
func readOnlyAgentConstruction(reg *tool.Registry, opts Options) (*tool.Registry, Options) {
	opts.ReadOnlyExecution = true
	opts.PlannerMCPExecution = false
	return strictReadOnlyExecutionRegistry(reg), opts
}

// NewPlannerAgent constructs the interactive two-model planner: permanent
// ReadOnlyExecution still blocks bash, file writers, and ordinary non-MCP
// writers, while PlannerMCPExecution allows authorized, non-destructive MCP
// through the stable use_capability proxy without requiring readOnlyHint.
func NewPlannerAgent(prov provider.Provider, reg *tool.Registry, sess *sessionstore.Session, opts Options, sink event.Sink) *Agent {
	opts.EventSource = event.UsageSourcePlanner
	opts.ReadOnlyExecution = true
	opts.PlannerMCPExecution = true
	// The coordinator needs visible plan text to hand off to the executor;
	// reasoning shown in a frontend is not a substitute for that contract.
	opts.RequireVisibleFinal = true
	// Keep construction-time filter for ordinary tools; use_capability stays
	// because it is ReadOnly. Direct mcp__* tools are already excluded by
	// PlannerToolRegistry. Dynamic MCP targets are re-checked after resolve.
	reg = plannerExecutionRegistry(reg)
	return New(prov, reg, sess, opts, sink)
}

// plannerExecutionRegistry is the construction-time filter for NewPlannerAgent.
// It removes ordinary writers and destructive direct MCP tools while keeping
// use_capability and built-in research tools. Host-starting deferred MCP
// targets are allowed at execution time under PlannerMCPExecution.
func plannerExecutionRegistry(reg *tool.Registry) *tool.Registry {
	filtered := tool.NewRegistry()
	if reg == nil {
		return filtered
	}
	filtered.CopyDisabledMCPFrom(reg)
	for _, name := range reg.Names() {
		target, ok := reg.Get(name)
		if !ok {
			continue
		}
		if name == "use_capability" {
			filtered.Add(target)
			continue
		}
		if strings.HasPrefix(name, tool.MCPNamePrefix) {
			// Defense in depth: planner never exposes direct MCP schemas.
			continue
		}
		if !target.ReadOnly() || tool.HasMCPDestructiveHint(target) {
			continue
		}
		if h, ok := target.(tool.ReadOnlyExecutionHostMutation); ok && h.ReadOnlyExecutionHostMutation() {
			// Ordinary host mutations stay out; MCP startup is only via proxy.
			continue
		}
		filtered.Add(target)
	}
	return filtered
}

// RunReadOnlySubAgentWithSession is the construction boundary for every
// strictly read-only child loop. Registry filtering limits the visible surface;
// this permanent execution flag also re-checks targets resolved dynamically by
// proxy tools such as use_capability. It never enables PlannerMCPExecution.
func RunReadOnlySubAgentWithSession(ctx context.Context, prov provider.Provider, reg *tool.Registry, sess *sessionstore.Session, prompt string, opts Options, sink event.Sink) (string, error) {
	reg, opts = readOnlyAgentConstruction(reg, opts)
	return RunSubAgentWithSession(ctx, prov, reg, sess, prompt, opts, sink)
}

// strictReadOnlyExecutionRegistry is the final construction-time filter shared
// by every strict child. Callers still apply role-specific filtering (review,
// planner, profile allowlists), while this layer guarantees that a missed call
// site cannot expose writers, destructive MCP tools, readers from unauthorized
// servers, or an unauthorized host-starting target to the model.
func strictReadOnlyExecutionRegistry(reg *tool.Registry) *tool.Registry {
	filtered := tool.NewRegistry()
	if reg == nil {
		return filtered
	}
	filtered.CopyDisabledMCPFrom(reg)
	for _, name := range reg.Names() {
		target, ok := reg.Get(name)
		if !ok || !target.ReadOnly() || tool.HasMCPDestructiveHint(target) {
			continue
		}
		if isInstalledMCPTool(target) && !tool.IsMCPServerAuthorized(target) {
			continue
		}
		if mutation, ok := target.(tool.ReadOnlyExecutionHostMutation); ok && mutation.ReadOnlyExecutionHostMutation() && !readOnlyExecutionAllowsMCPStartup(target) {
			continue
		}
		filtered.Add(target)
	}
	return filtered
}

// latestAssistantAnswer walks the session backwards for the last assistant
// message with content — that's the sub-agent's final answer. Intermediate
// assistant messages with tool_calls but no text don't count.
func latestAssistantAnswer(sess *sessionstore.Session) string {
	if sess == nil {
		return ""
	}
	for _, v := range slices.Backward(sess.Messages) {
		m := v
		if m.Role == provider.RoleAssistant && strings.TrimSpace(m.Content) != "" {
			return m.Content
		}
	}
	return ""
}

// dumpFailedSubagentSession best-effort persists a failed report-required
// subagent transcript for post-hoc diagnosis (read-only skill subagents are
// otherwise ephemeral, so a protocol failure leaves no trace). Returns a
// human-readable suffix naming the dump, or "" when disabled/failed.
func dumpFailedSubagentSession(archiveDir, kind string, sess *sessionstore.Session) string {
	if strings.TrimSpace(archiveDir) == "" || sess == nil {
		return ""
	}
	dir := filepath.Join(archiveDir, "subagent-report-failures")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return ""
	}
	path := filepath.Join(dir, fmt.Sprintf("%s-%d.jsonl", kind, time.Now().UnixNano()))
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0o600)
	if err != nil {
		return ""
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, m := range sess.Messages {
		if err := enc.Encode(m); err != nil {
			return ""
		}
	}
	return "; transcript dumped to " + path
}

// mergeChildEvidence folds a sub-agent's real receipts into the parent ledger
// carried on ctx. Meta tools themselves are never mutations.
func mergeChildEvidence(ctx context.Context, sub *Agent) {
	if sub == nil {
		return
	}
	parent, ok := evidence.FromContext(ctx)
	if !ok || parent == nil {
		return
	}
	parent.MergeChild(sub.EvidenceSummary())
}

func isFreshSubagentSession(sess *sessionstore.Session) bool {
	if sess == nil {
		return false
	}
	snap := sess.Snapshot()
	return len(snap) == 1 && snap[0].Role == provider.RoleSystem
}

// EvidenceSummary exports this agent's turn-scoped receipts for parent merge.
func (a *Agent) EvidenceSummary() evidence.ChildEvidenceSummary {
	if a == nil || a.task.ledger == nil {
		return evidence.ChildEvidenceSummary{}
	}
	return a.task.ledger.Summary()
}

// composeSubagentAnswer assembles everything the parent is shown for one child
// run: the host-adjudicated completion claim when the child submitted one, the
// child's own prose, then the host's receipts.
func composeSubagentAnswer(ctx context.Context, answer string, sub *Agent, claims writeclaim.WritePathSet, delegationText string) string {
	summary := sub.EvidenceSummary()
	report, reasons, hasReport := sub.CompletionReport()
	if hasReport {
		answer = strings.TrimSpace(formatCompletionReport(report, reasons) + "\n\n" + answer)
	}
	recordDelegationAudit(ctx, summary, claims, report, reasons, hasReport, delegationText)
	return AppendHostReceipts(answer, summary, claims)
}

// maxReviewReportNudges bounds the in-session completion nudges sent to a
// review subagent that finished without submitting review_report. Each nudge is
// one cheap continuation request on the same (cached) subagent session — far
// cheaper than discarding the run and re-reviewing from scratch.
// maxReviewReportNudges is the single in-session retry after a review run that
// produced no typed report. One is the useful number: the nudge asks only for
// the submission, and a second refusal means the tool is unreachable rather
// than forgotten, which no further asking fixes.
const maxReviewReportNudges = 1

// newDelegatedAgent builds the child a delegated run executes as, wired from
// the call context rather than from what a caller remembered to pass. Only a
// delegated run gets an asker: its parent is blocked and a person is waiting,
// while fleet items, parallel tasks and background jobs stay silent.
func newDelegatedAgent(ctx context.Context, prov provider.Provider, reg *tool.Registry,
	sess *sessionstore.Session, opts Options, sink event.Sink,
) *Agent {
	sub := New(prov, reg, sess, opts, sink)
	if _, _, asker, ok := CallContext(ctx); ok && asker != nil {
		sub.SetAsker(asker)
	}
	return sub
}

// observeSubagentHandoff records one child run's closing protocol. Called from
// a single defer so every exit path is counted the same: a salvage, a review
// failure and a clean answer are all runs that either submitted a report or
// did not.
func observeSubagentHandoff(sink event.Sink, sub *Agent, sess *sessionstore.Session, opts Options, answer string, runErr error) {
	if sub == nil || sess == nil {
		return
	}
	audit := event.SubagentHandoffAudit{
		Entrance: opts.HandoffEntrance,
		Depth:    opts.SubagentDepth,
		ReadOnly: opts.ReadOnlyExecution,
		Expected: opts.ExpectCompletionReport,
		Exit:     handoffExit(answer, runErr),
	}
	countReportCalls(&audit, sess.Snapshot())
	// Claimed and adjudicated are both kept: a child that submits reports
	// readily and has them lowered every time is a different problem from one
	// that does not submit, and a single status cannot say which.
	if sub.task.ledger != nil {
		verdicts := sub.task.ledger.ClosureVerdicts()
		audit.Closed, audit.NeedsWork = verdicts.Closed, verdicts.NeedsWork
		if claimed, ok := sub.task.ledger.LatestCompletionReport(); ok {
			adjudicated, reasons := sub.task.ledger.AdjudicateCompletion(claimed)
			audit.ClaimedStatus = string(claimed.Status)
			audit.AdjudicatedStatus = string(adjudicated.Status)
			audit.LoweredClaims = len(reasons)
			audit.Criteria = len(adjudicated.Criteria)
			audit.Unresolved = len(adjudicated.Unresolved)
			for _, c := range adjudicated.Criteria {
				audit.Evidence += len(c.Evidence)
			}
		}
	}
	event.RecordSubagentHandoff(sink, audit)
}

// reviewReportNudgePrompt asks an already-finished review subagent to submit
// the missing typed report without redoing the review.
func reviewReportNudgePrompt(kind evidence.ReviewKind) string {
	return fmt.Sprintf("You finished the review without calling the review_report tool, so the host cannot accept the run yet. Do not redo the review. Call review_report now with kind=%q, your verdict (pass | warn | block), reviewed_paths listing only the files you actually read in this conversation, and the findings you already reported. Then restate your final verdict in one sentence.", string(kind))
}

// reviewReportTaskContract is appended to the task prompt of a review subagent
// whose run must end with a typed report. The skill body describes how to
// review; this states the non-negotiable submission protocol.
func reviewReportTaskContract(kind evidence.ReviewKind) string {
	return fmt.Sprintf(`<review-report-contract event="SubagentReviewReport">
Before your final answer you MUST call the review_report tool exactly once with kind=%q, your verdict (pass | warn | block), reviewed_paths listing only files you actually read this run, and your findings. The host discards a review run that ends without a successful review_report call — your prose summary alone does not count.
</review-report-contract>`, string(kind))
}

// reviewWithoutVerdictNote marks a review the host has no typed verdict for, so
// the prose is not read as one the gate weighed and let through.
const reviewWithoutVerdictNote = "[no recorded verdict] The reviewer did not submit a review_report, so the host holds no verdict for this run: nothing below was checked against its receipts, and a blocking finding here will not stop delivery on its own.\n\n"

const subagentStartContext = `<subagent-context event="SubagentStart">
Before acting, check the available skills and tools. If a relevant skill is available, invoke it before continuing. Delegate to another sub-agent only when the task genuinely benefits from isolated context and the delegation tool is available.
</subagent-context>`

// withSubagentSessionTemp installs a fresh session-private temporary directory
// Manager for one sub-agent run. The returned release must be deferred by the
// caller so the directory is retired when the run ends (including background
// sub-agent completion).
func withSubagentSessionTemp(ctx context.Context) (context.Context, func()) {
	m := sessiontemp.New()
	m.Retain()
	return sessiontemp.WithManager(ctx, m), m.Release
}

// mutationObserverSetter is a tool that spawns sub-agents and hands them the
// parent's mutation observer.
type mutationObserverSetter interface {
	SetMutationObserver(*checkpoint.MutationObserver)
}

// formatCompletionReport renders the child's claim after the host has lowered
// whatever its receipts could not back. Downgrades are shown, never silently
// applied: a parent that cannot see the adjudication cannot trust the status.
func formatCompletionReport(report evidence.CompletionReport, reasons []string) string {
	var b strings.Builder
	b.WriteString("status: ")
	b.WriteString(string(report.Status))
	if len(reasons) > 0 {
		b.WriteString(" (lowered by the host: unbacked criterion claims)")
	}
	b.WriteString("\nsummary: ")
	b.WriteString(report.Summary)
	for _, c := range report.Criteria {
		b.WriteString("\n  " + c.ID + " " + string(c.Status))
		if proof := criterionProof(c); proof != "" {
			b.WriteString(" — " + proof)
		}
	}
	for _, reason := range reasons {
		b.WriteString("\n  host lowered " + reason)
	}
	for _, u := range report.Unresolved {
		b.WriteString("\nunresolved: " + u)
	}
	return b.String()
}

// recordDelegationAudit emits one structured receipt per child run. It reports
// what the host observed and what it refused to back, so an orchestration
// benchmark can separate real gains from extra tokens spent. delegationText is
// the parent-authored task before host framing, which is what makes the
// evidence-origin split a host record rather than a claim.
func recordDelegationAudit(ctx context.Context, summary evidence.ChildEvidenceSummary, claims writeclaim.WritePathSet, report evidence.CompletionReport, reasons []string, hasReport bool, delegationText string) {
	audit := hostaudit.DelegationAudit{
		Depth:           SubagentDepth(ctx),
		ToolCalls:       len(summary.Receipts),
		MutationPaths:   summary.MutationPaths(),
		ClaimViolations: len(claimViolations(summary, claims)),
		HasReport:       hasReport,
		Downgrades:      len(reasons),
	}
	audit.Mutations = len(audit.MutationPaths)
	evidence.ClassifyEvidenceOrigin(&audit, delegationText, summary.EvidencePaths())
	if hasReport {
		audit.AdjudicatedStatus = string(report.Status)
	}
	_, sink, _, _ := CallContext(ctx)
	event.RecordDelegationAudit(sink, audit)
}

// AppendHostReceipts attaches the host's own attestation to a child's answer.
// A child cannot write, suppress, or contradict these lines. An empty block is
// omitted entirely, so read-only research children stay exactly as cheap as
// they were before.
func AppendHostReceipts(answer string, summary evidence.ChildEvidenceSummary, claims writeclaim.WritePathSet) string {
	block := formatHostReceipts(summary, claims)
	if block == "" {
		return answer
	}
	if strings.TrimSpace(answer) == "" {
		return block
	}
	return answer + "\n\n" + block
}

// handoffExit classifies how the run ended. A provider failure that produced no
// report is not a compliance failure, and counting it as one would make the
// denominator answer a different question.
func handoffExit(answer string, runErr error) string {
	switch {
	case errors.Is(runErr, context.Canceled):
		return "cancelled"
	case runErr != nil:
		return "error"
	case strings.TrimSpace(answer) == "":
		return "no_answer"
	default:
		return "completed"
	}
}

// countReportCalls walks the child's transcript for complete_subtask calls and
// what the tool made of them. Attempted-and-refused is a schema problem;
// never-attempted is a protocol one, and only the counts can tell them apart.
func countReportCalls(audit *event.SubagentHandoffAudit, msgs []provider.Message) {
	calls := map[string]bool{}
	round := 0
	for _, m := range msgs {
		if len(m.ToolCalls) > 0 {
			round++
		}
		for _, tc := range m.ToolCalls {
			if tc.Name != completeSubtaskToolName {
				if audit.ReportRound > 0 {
					audit.ToolCallsAfterReport++
				}
				continue
			}
			calls[tc.ID] = true
			audit.Attempts++
			if audit.ReportRound == 0 {
				audit.ReportRound = round
			}
		}
		if m.Role != provider.RoleTool || !calls[m.ToolCallID] {
			continue
		}
		if isErrorMessage(m) {
			audit.Malformed++
			continue
		}
		audit.Accepted++
	}
	audit.FinalRound = round
}

// claimViolations returns the mutations the host observed outside the write
// claim the child declared. Tool-level confinement already refuses these, so a
// non-empty result means a write reached the workspace through a surface the
// claim could not bind — the parent must not treat the run as scoped.
func claimViolations(summary evidence.ChildEvidenceSummary, claims writeclaim.WritePathSet) []string {
	if claims.Empty() {
		return nil
	}
	var out []string
	for _, p := range summary.MutationPaths() {
		if !claims.AllowsPath(p) {
			out = append(out, p)
		}
	}
	return out
}

func criterionProof(c evidence.AcceptanceCriterion) string {
	var parts []string
	for _, e := range c.Evidence {
		switch {
		case strings.TrimSpace(e.Command) != "":
			parts = append(parts, e.Command)
		case len(e.Paths) > 0:
			parts = append(parts, strings.Join(e.Paths, " "))
		case strings.TrimSpace(e.Summary) != "":
			parts = append(parts, e.Kind+": "+e.Summary)
		}
	}
	return joinBoundedReceipts(parts)
}

// formatHostReceipts renders only what the host is willing to attest to: files
// the child really changed, and commands whose outcome the host observed.
// Ordinary reads and greps are excluded on purpose — they are not claims a
// parent has to adjudicate, and every rendered line costs parent context.
func formatHostReceipts(summary evidence.ChildEvidenceSummary, claims writeclaim.WritePathSet) string {
	changed := summary.MutationPaths()
	commands := hostReceiptCommands(summary)
	violations := claimViolations(summary, claims)
	if len(changed) == 0 && len(commands) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(HostReceiptsHeader)
	if len(changed) > 0 {
		b.WriteString("\n  changed: ")
		b.WriteString(joinBoundedReceipts(changed))
	}
	if len(commands) > 0 {
		b.WriteString("\n  commands: ")
		b.WriteString(joinBoundedReceipts(commands))
	}
	if len(violations) > 0 {
		b.WriteString("\n  " + HostReceiptsViolationLabel + ": ")
		b.WriteString(joinBoundedReceipts(violations))
	}
	return b.String()
}

// hostReceiptCommands keeps only shell receipts carrying an outcome worth
// attesting: a host-classified verification, or a command that did not succeed.
func hostReceiptCommands(summary evidence.ChildEvidenceSummary) []string {
	var out []string
	seen := map[string]bool{}
	for _, r := range summary.Receipts {
		cmd := strings.TrimSpace(r.Command)
		outcome := hostReceiptOutcome(r)
		if cmd == "" || outcome == "" || seen[cmd] {
			continue
		}
		seen[cmd] = true
		out = append(out, cmd+outcome)
	}
	return out
}

const HostReceiptsHeader = "Host receipts (recorded by the host as the sub-agent ran, not claimed by it):"

const HostReceiptsViolationLabel = "OUTSIDE DECLARED write_paths"

func joinBoundedReceipts(items []string) string {
	if len(items) <= hostReceiptsMaxItems {
		return strings.Join(items, ", ")
	}
	return strings.Join(items[:hostReceiptsMaxItems], ", ") +
		fmt.Sprintf(" (+%d more)", len(items)-hostReceiptsMaxItems)
}

func hostReceiptOutcome(r evidence.Receipt) string {
	var parts []string
	switch r.Verification {
	case evidence.VerificationPassed:
		parts = append(parts, "verification passed")
	case evidence.VerificationFailed:
		parts = append(parts, "verification failed")
	}
	switch {
	case r.ExitCode != nil && (*r.ExitCode != 0 || len(parts) > 0):
		parts = append(parts, fmt.Sprintf("exit %d", *r.ExitCode))
	case r.ExitCode == nil && !r.Success:
		parts = append(parts, "did not complete")
	}
	if len(parts) == 0 {
		return ""
	}
	return " (" + strings.Join(parts, ", ") + ")"
}

// hostReceiptsMaxItems bounds each rendered line so a long child run cannot
// crowd the parent's context out with paths and commands.
const hostReceiptsMaxItems = 8
