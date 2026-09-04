// Package eventwire defines the shared frontend JSON contract for event.Event.
package eventwire

import (
	"reasonix/internal/agentgraph"
	"reasonix/internal/billing"
	"reasonix/internal/event"
	"reasonix/internal/provider"
)

// Event is the JSON-friendly form shared by event frontends.
// externalizable:"true" marks large string payloads the Remote protocol may
// offload via content refs without changing provider-visible semantics.
type Event struct {
	Kind            string           `json:"kind"`
	Text            string           `json:"text,omitempty" externalizable:"true"`
	Detail          string           `json:"detail,omitempty" externalizable:"true"`
	Code            string           `json:"code,omitempty"`
	Reasoning       string           `json:"reasoning,omitempty" externalizable:"true"`
	MemoryCitations []MemoryCitation `json:"memoryCitations,omitempty"`
	Level           string           `json:"level,omitempty"`
	// Audience separates a notice about this conversation from one about the
	// machine running it. Only the first belongs in a transcript.
	Audience        string              `json:"audience,omitempty"`
	Tool            *Tool               `json:"tool,omitempty"`
	Usage           *Usage              `json:"usage,omitempty"`
	Approval        *Approval           `json:"approval,omitempty"`
	Ask             *Ask                `json:"ask,omitempty"`
	Compaction      *Compaction         `json:"compaction,omitempty"`
	Maintenance     *ContextMaintenance `json:"maintenance,omitempty"`
	TodoProgress    *TodoProgress       `json:"todoProgress,omitempty"`
	Guardian        *Guardian           `json:"guardian,omitempty"`
	DecisionReceipt *DecisionReceipt    `json:"decisionReceipt,omitempty"`
	Extension       *ExtensionSurface   `json:"extension,omitempty"`
	Err             string              `json:"err,omitempty" externalizable:"true"`
	Cancelled       bool                `json:"cancelled,omitempty"` // TurnDone: the user asked to stop
	Outcome         string              `json:"outcome,omitempty"`
	Readiness       *FinalReadiness     `json:"readiness,omitempty"`
	Receipt         *CompletionReceipt  `json:"receipt,omitempty"`
	CheckpointTurn  *int                `json:"checkpointTurn,omitempty"`
	RetryAttempt    int                 `json:"retryAttempt,omitempty"`
	RetryMax        int                 `json:"retryMax,omitempty"`
	RetryScope      string              `json:"retryScope,omitempty"` // "headers" | "stream"; omit for older clients
	StreamAttempt   *StreamAttempt      `json:"streamAttempt,omitempty"`
	// ItemID correlates Steer / TurnDone / unapplied-steer with a durable
	// session-inbox entry. Empty for legacy text-only guidance.
	ItemID    string            `json:"itemId,omitempty"`
	Workspace *WorkspaceChanged `json:"workspace,omitempty"`
	// Phase is set on turn_phase events: working | checking | verifying | reviewing.
	Phase string `json:"phase,omitempty"`
	// Completion is set on completion_summary events (content-free quality summary).
	Completion *CompletionSummary `json:"completion,omitempty"`
	// Graph is set on graph_delta events. The leaf type is the contract on both
	// sides of the wire, so re-declaring its shape here would reintroduce the
	// second description of the graph this package exists to remove.
	Graph *agentgraph.Delta `json:"graph,omitempty"`
	// Seq numbers frames a client must not miss, so a resume can ask for what it
	// missed. Streaming deltas carry none — the same reason SSE omits `id:` on a
	// frame that should not move Last-Event-ID. Set by the transport.
	Seq int64 `json:"seq,omitempty"`
}

// CompletionSummary is the JSON form of event.CompletionSummaryInfo.
type CompletionSummary struct {
	Preset           string   `json:"preset"`
	Verdict          string   `json:"verdict"`
	Mutations        int      `json:"mutations"`
	ChecksPassed     int      `json:"checks_passed"`
	ChecksFailed     int      `json:"checks_failed"`
	ChecksSuppressed int      `json:"checks_suppressed"`
	Review           string   `json:"review"`
	GapKinds         []string `json:"gap_kinds,omitempty"`
	// CriteriaRewritten names existing tests the turn rewrote or removed.
	CriteriaRewritten []string `json:"criteria_rewritten,omitempty"`
}

type WorkspaceChanged struct {
	Revisions  WorkspaceRevision     `json:"revisions"`
	Changes    []WorkspacePathChange `json:"changes"`
	AllPaths   bool                  `json:"allPaths"`
	Source     string                `json:"source"`
	WatchState string                `json:"watchState"`
}

type WorkspaceRevision struct {
	Content     uint64 `json:"content"`
	Tree        uint64 `json:"tree"`
	WorkingTree uint64 `json:"workingTree"`
	GitMeta     uint64 `json:"gitMeta"`
	Session     uint64 `json:"session"`
}

type WorkspacePathChange struct {
	Path    string `json:"path"`
	OldPath string `json:"oldPath,omitempty"`
	Op      string `json:"op"`
}

// StreamAttempt is the JSON form of event.StreamAttemptInfo.
type StreamAttempt struct {
	ID      string `json:"id"`
	Action  string `json:"action"` // begin | discard | commit
	Attempt int    `json:"attempt,omitempty"`
	Max     int    `json:"max,omitempty"`
	Reason  string `json:"reason,omitempty"` // connection_reset | premature_eof | idle_timeout
}

// ToWire converts a typed runtime event into the shared frontend JSON contract.
func ToWire(e event.Event) Event {
	w := Event{Kind: kindNames[e.Kind], Text: e.Text, Detail: e.Detail, Reasoning: e.Reasoning, ItemID: e.ItemID}
	if len(e.MemoryCitations) > 0 {
		w.MemoryCitations = ToWireMemoryCitations(e.MemoryCitations)
	}
	switch e.Kind {
	case event.Notice:
		w.Code = e.Code
		if e.DecisionReceipt != nil {
			w.DecisionReceipt = ToWireDecisionReceipt(e.DecisionReceipt)
		}
		w.Audience = string(e.Audience)
		if e.Level == event.LevelWarn {
			w.Level = "warn"
		} else {
			w.Level = "info"
		}
	case event.ToolDispatch, event.ToolResult, event.ToolProgress:
		w.Tool = toWireTool(e.Tool)
	case event.GraphDelta:
		w.Graph = e.Graph
	case event.WorkspaceChanged:
		w.Workspace = toWireWorkspace(e.Workspace)
	case event.Usage:
		w.Usage = toWireUsage(e)
	case event.ApprovalRequest:
		w.Approval = &Approval{
			ID: e.Approval.ID, Tool: e.Approval.Tool, Subject: e.Approval.Subject,
			Reason: e.Approval.Reason, Fresh: e.Approval.Fresh, Kind: e.Approval.Kind,
		}
		if e.Approval.Recovery != nil {
			r := e.Approval.Recovery
			w.Approval.Recovery = &RecoveryApproval{
				SourceAgent:     r.SourceAgent,
				FailedTool:      r.FailedTool,
				FailedSummary:   r.FailedSummary,
				Diagnosis:       r.Diagnosis,
				NextTool:        r.NextTool,
				NextAction:      r.NextAction,
				ChangeKind:      r.ChangeKind,
				ChangeRationale: r.ChangeRationale,
				ReviewRationale: r.ReviewRationale,
				PlanBefore:      r.PlanBefore,
				PlanAfter:       r.PlanAfter,
				CanGrantTask:    r.CanGrantTask,
				TaskGrantScope:  r.TaskGrantScope,
			}
		}
	case event.AskRequest:
		w.Ask = ToWireAsk(e.Ask)
	case event.CompactionStarted, event.CompactionDone:
		w.Compaction = toWireCompaction(e.Compaction)
	case event.ContextMaintenanceEvent:
		if m := e.Maintenance; m != nil {
			w.Maintenance = &ContextMaintenance{
				Status: m.Status, Action: m.Action, Trigger: m.Trigger,
				OperationID: m.OperationID, InputTokens: m.InputTokens,
				ResultTokens: m.ResultTokens, SavedTokens: m.SavedTokens,
				AffectedToolResults: m.AffectedToolResults,
				ProjectionVersion:   m.ProjectionVersion, CacheBreak: m.CacheBreak,
				Reason: m.Reason, Code: m.Code, Boundary: m.Boundary,
				TriggerTokens: m.TriggerTokens,
			}
		}
	case event.TodoProgressEvent:
		w.TodoProgress = toWireTodoProgress(e.TodoProgress)
	case event.GuardianAssessment:
		w.Guardian = ToWireGuardian(e.Guardian)
	case event.ExtensionSurface, event.ExtensionStatus:
		w.Extension = ToWireExtensionSurface(e.Extension)
	case event.TurnDone:
		w.Cancelled = e.Cancelled
		w.Outcome = e.Outcome
		w.CheckpointTurn = e.CheckpointTurn
		w.Receipt = completionReceiptWire(e.Receipt)
		if e.Readiness != nil {
			w.Readiness = &FinalReadiness{Attempts: e.Readiness.Attempts, Missing: append([]string(nil), e.Readiness.Missing...)}
		}
		if e.Err != nil {
			w.Err = e.Err.Error()
		}
	case event.Retrying:
		w.RetryAttempt = e.RetryAttempt
		w.RetryMax = e.RetryMax
		if e.RetryScope != "" {
			w.RetryScope = string(e.RetryScope)
		}
	case event.StreamAttempt:
		w.StreamAttempt = &StreamAttempt{
			ID:      e.StreamAttempt.ID,
			Action:  string(e.StreamAttempt.Action),
			Attempt: e.StreamAttempt.Attempt,
			Max:     e.StreamAttempt.Max,
			Reason:  e.StreamAttempt.Reason,
		}
	case event.TurnPhase:
		w.Phase = string(e.PhaseName)
		if w.Phase == "" {
			w.Phase = e.Text
		}
	case event.CompletionSummary:
		if c := e.Completion; c != nil {
			w.Completion = &CompletionSummary{
				Preset:            c.Preset,
				Verdict:           c.Verdict,
				Mutations:         c.Mutations,
				ChecksPassed:      c.ChecksPassed,
				ChecksFailed:      c.ChecksFailed,
				ChecksSuppressed:  c.ChecksSuppressed,
				Review:            c.Review,
				GapKinds:          append([]string(nil), c.GapKinds...),
				CriteriaRewritten: append([]string(nil), c.CriteriaRewritten...),
			}
		}
	}
	return w
}

func toWireUsage(e event.Event) *Usage {
	u := e.Usage
	if u == nil {
		return nil
	}
	wire := &Usage{
		PromptTokens: u.PromptTokens, CompletionTokens: u.CompletionTokens,
		TotalTokens: u.TotalTokens, CacheHitTokens: u.CacheHitTokens,
		CacheMissTokens: u.CacheMissTokens, ReasoningTokens: u.ReasoningTokens,
		Estimated:               u.Estimated,
		Source:                  e.UsageSource,
		AttemptID:               e.AttemptID,
		ContextPromptTokens:     u.ContextPromptTokens,
		ContextCompletionTokens: u.ContextCompletionTokens,
		ContextReasoningTokens:  u.ContextReasoningTokens,
		ContextCacheHitTokens:   u.ContextCacheHitTokens,
		ContextCacheMissTokens:  u.ContextCacheMissTokens,
		SessionCacheHitTokens:   e.SessionHit, SessionCacheMissTokens: e.SessionMiss,
	}
	if e.CacheDiagnostics != nil {
		wire.CacheDiagnostics = ToWireCacheDiagnostics(e.CacheDiagnostics)
	}
	quote := e.CostQuote
	if quote == nil && e.Pricing != nil {
		quote = event.EnsureCostQuote(e, nil)
	}
	if quote != nil {
		wire.CostQuote = quote
		wire.CostComplete = quote.CostComplete
		wire.DisplayComplete = quote.DisplayComplete
		wire.DisplayStatus = quote.DisplayStatus
		wire.AggregateMode = quote.AggregateMode
		wire.OriginalTotals = append([]billing.Money(nil), quote.OriginalTotals...)
		if quote.Selected != nil {
			wire.Cost = quote.Selected.Float64()
			wire.Currency = quote.LegacyCurrencySymbol()
			wire.CostUSD = wire.Cost
			wire.CurrencyCode = quote.LegacyCurrencyCode()
		}
	}
	return wire
}

// DecisionReceipt is the JSON form of a provider-excluded user decision.
type DecisionReceipt struct {
	ID      string `json:"id"`
	Kind    string `json:"kind"`
	Tool    string `json:"tool,omitempty"`
	Subject string `json:"subject,omitempty"`
	Outcome string `json:"outcome"`
}

func ToWireDecisionReceipt(in *provider.DecisionReceipt) *DecisionReceipt {
	if in == nil {
		return nil
	}
	return &DecisionReceipt{ID: in.ID, Kind: in.Kind, Tool: in.Tool, Subject: in.Subject, Outcome: in.Outcome}
}

type FinalReadiness struct {
	Attempts int      `json:"attempts,omitempty"`
	Missing  []string `json:"missing,omitempty"`
}

// MemoryCitation is the JSON form of provider.MemoryCitation.
type MemoryCitation struct {
	ID        string `json:"id,omitempty"`
	Source    string `json:"source"`
	LineStart int    `json:"lineStart,omitempty"`
	LineEnd   int    `json:"lineEnd,omitempty"`
	Note      string `json:"note,omitempty"`
	Kind      string `json:"kind,omitempty"`
}

// ToWireMemoryCitations converts local memory references into frontend JSON.
func ToWireMemoryCitations(in []provider.MemoryCitation) []MemoryCitation {
	out := make([]MemoryCitation, 0, len(in))
	for _, c := range in {
		if c.Source == "" && c.ID == "" && c.Note == "" {
			continue
		}
		out = append(out, MemoryCitation{
			ID:        c.ID,
			Source:    c.Source,
			LineStart: c.LineStart,
			LineEnd:   c.LineEnd,
			Note:      c.Note,
			Kind:      c.Kind,
		})
	}
	return out
}

// Compaction is the JSON form of an event.Compaction.
type Compaction struct {
	Trigger  string `json:"trigger,omitempty"`
	Messages int    `json:"messages,omitempty"`
	Summary  string `json:"summary,omitempty" externalizable:"true"`
	// What the fold cost, and what the digest kept of it.
	SourceTokens     int  `json:"sourceTokens,omitempty"`
	ProjectionTokens int  `json:"projectionTokens,omitempty"`
	CoverageRequired int  `json:"coverageRequired,omitempty"`
	CoverageMissing  int  `json:"coverageMissing,omitempty"`
	CoverageRepaired bool `json:"coverageRepaired,omitempty"`
	// The host completed a digest the summarizer left incomplete.
	CoverageBackstopped bool `json:"coverageBackstopped,omitempty"`
}

func toWireCompaction(c event.Compaction) *Compaction {
	return &Compaction{
		Trigger: c.Trigger, Messages: c.Messages,
		Summary:      c.Summary,
		SourceTokens: c.SourceTokens, ProjectionTokens: c.ProjectionTokens,
		CoverageRequired: c.CoverageRequired, CoverageMissing: c.CoverageMissing,
		CoverageRepaired: c.CoverageRepaired, CoverageBackstopped: c.CoverageBackstopped,
	}
}

// AskOption is one JSON-formatted choice in a structured ask request.
type AskOption struct {
	Label       string `json:"label"`
	Description string `json:"description,omitempty" externalizable:"true"`
}

// AskQuestion is one JSON-formatted structured ask question.
type AskQuestion struct {
	ID      string      `json:"id"`
	Header  string      `json:"header,omitempty"`
	Prompt  string      `json:"prompt" externalizable:"true"`
	Reason  string      `json:"reason,omitempty"`
	Options []AskOption `json:"options"`
	Multi   bool        `json:"multi,omitempty"`
}

// Ask is the JSON form of an event.Ask.
type Ask struct {
	ID        string        `json:"id"`
	Questions []AskQuestion `json:"questions"`
}

// Profile carries the subagent model/effort resolved for a tool call.
type Profile struct {
	Name   string `json:"name,omitempty"`
	Count  int    `json:"count,omitempty"`
	Model  string `json:"model,omitempty"`
	Effort string `json:"effort,omitempty"`
}

// Bound is the JSON form of event.OutputBound, present only when the result did
// not arrive whole. "spilled" keeps Path, which a frontend can offer to open;
// "truncated" keeps KeptBytes, which is all the model ever saw.
type Bound struct {
	Kind      string `json:"kind"`
	Lines     int    `json:"lines,omitempty"`
	Bytes     int    `json:"bytes,omitempty"`
	KeptBytes int    `json:"keptBytes,omitempty"`
	Path      string `json:"path,omitempty"`
}

var boundKindNames = map[event.BoundKind]string{
	event.BoundSpilled:   "spilled",
	event.BoundWindowed:  "windowed",
	event.BoundTruncated: "truncated",
}

func toWireTool(t event.Tool) *Tool {
	wt := &Tool{
		ID: t.ID, Name: t.Name, Args: t.Args,
		ResolvedName: t.ResolvedName, CapabilityID: t.CapabilityID,
		Output: t.Output, Err: t.Err,
		ReadOnly: t.ReadOnly, Truncated: t.Bound.Lossy(),
		DurationMs: t.DurationMs, ContextTokens: t.ContextTokens(),
		Partial: t.Partial, StartedAt: t.StartedAt, EndedAt: t.EndedAt,
		ArgChars: t.ArgChars, Refreshed: t.Refreshed,
		ParentID: t.ParentID, AttemptID: t.AttemptID,
		Diff: t.Diff, Added: t.Added, Removed: t.Removed,
	}
	if b := t.Bound; b.Kind != event.BoundWhole {
		wt.Bound = &Bound{
			Kind: boundKindNames[b.Kind], Lines: b.Lines,
			Bytes: b.Bytes, KeptBytes: b.KeptBytes, Path: b.Path,
		}
	}
	if t.Profile != nil {
		wt.Profile = &Profile{Name: t.Profile.Name, Count: t.Profile.Count, Model: t.Profile.Model, Effort: t.Profile.Effort}
	}
	if t.Execution != nil {
		wt.Execution = toWireShellExecution(t.Execution)
	}
	return wt
}

// toWireWorkspace renders a workspace mutation. A nil payload still renders,
// so a frontend reading the field never has to tell "no changes" apart from
// "the event arrived without one".
func toWireWorkspace(ws *event.WorkspaceChangedPayload) *WorkspaceChanged {
	if ws == nil {
		ws = &event.WorkspaceChangedPayload{}
	}
	changes := make([]WorkspacePathChange, 0, len(ws.Changes))
	for _, c := range ws.Changes {
		changes = append(changes, WorkspacePathChange{Path: c.Path, OldPath: c.OldPath, Op: c.Op})
	}
	return &WorkspaceChanged{
		Revisions: WorkspaceRevision{Content: ws.Revisions.Content, Tree: ws.Revisions.Tree, WorkingTree: ws.Revisions.WorkingTree, GitMeta: ws.Revisions.GitMeta, Session: ws.Revisions.Session},
		Changes:   changes, AllPaths: ws.AllPaths, Source: ws.Source, WatchState: string(ws.WatchState),
	}
}

// Tool is the JSON form of an event.Tool.
type Tool struct {
	ID           string `json:"id,omitempty"`
	Name         string `json:"name"`
	Args         string `json:"args,omitempty" externalizable:"true"`
	ResolvedName string `json:"resolvedName,omitempty"`
	CapabilityID string `json:"capabilityId,omitempty"`
	Output       string `json:"output,omitempty" externalizable:"true"`
	Err          string `json:"err,omitempty" externalizable:"true"`
	ReadOnly     bool   `json:"readOnly"`
	// Truncated is the compatibility projection of Bound for journals written
	// before it existed; Bound is what a current frontend reads.
	Truncated  bool   `json:"truncated,omitempty"`
	Bound      *Bound `json:"bound,omitempty"`
	DurationMs int64  `json:"durationMs,omitempty"`
	// ContextTokens is what this call left in the prompt (args + result), so a
	// card can say which step is eating the window. Estimated, never billed.
	ContextTokens int             `json:"contextTokens,omitempty"`
	StartedAt     int64           `json:"startedAt,omitempty"` // unix ms; zero when the call never ran
	EndedAt       int64           `json:"endedAt,omitempty"`
	Partial       bool            `json:"partial,omitempty"`
	ArgChars      int             `json:"argChars,omitempty"`
	Refreshed     bool            `json:"refreshed,omitempty"`
	ParentID      string          `json:"parentId,omitempty"`
	AttemptID     string          `json:"attemptId,omitempty"` // host-local stream_attempt id for speculative partials
	Diff          string          `json:"diff,omitempty" externalizable:"true"`
	Added         int             `json:"added,omitempty"`
	Removed       int             `json:"removed,omitempty"`
	Profile       *Profile        `json:"profile,omitempty"`
	Execution     *ShellExecution `json:"execution,omitempty"`
}

// ShellExecution is the JSON form of event.ShellExecution (local UI metadata).
type ShellExecution struct {
	Kind           string `json:"kind,omitempty"`
	Shell          string `json:"shell,omitempty"`
	ShellVersion   string `json:"shellVersion,omitempty"`
	Platform       string `json:"platform,omitempty"`
	SupportsAndAnd bool   `json:"supportsAndAnd"`
	State          string `json:"state,omitempty"`
	FailurePhase   string `json:"failurePhase,omitempty"`
	ExitCode       *int   `json:"exitCode,omitempty"`
	OutputTail     string `json:"outputTail,omitempty"`
	MutationRisk   string `json:"mutationRisk,omitempty"`
	Verification   string `json:"verification,omitempty"`
	DurationMs     int64  `json:"durationMs,omitempty"`
}

func toWireShellExecution(in *event.ShellExecution) *ShellExecution {
	if in == nil {
		return nil
	}
	out := &ShellExecution{
		Kind: in.Kind, Shell: in.Shell, ShellVersion: in.ShellVersion,
		Platform: in.Platform, SupportsAndAnd: in.SupportsAndAnd,
		State: in.State, FailurePhase: in.FailurePhase,
		OutputTail: in.OutputTail, MutationRisk: in.MutationRisk,
		Verification: in.Verification, DurationMs: in.DurationMs,
	}
	if in.ExitCode != nil {
		code := *in.ExitCode
		out.ExitCode = &code
	}
	return out
}

// Usage is the JSON form of provider usage telemetry.
type Usage struct {
	PromptTokens     int               `json:"promptTokens"`
	CompletionTokens int               `json:"completionTokens"`
	TotalTokens      int               `json:"totalTokens"`
	CacheHitTokens   int               `json:"cacheHitTokens"`
	CacheMissTokens  int               `json:"cacheMissTokens"`
	ReasoningTokens  int               `json:"reasoningTokens,omitempty"`
	Estimated        bool              `json:"estimated,omitempty"`
	Source           string            `json:"source,omitempty"`
	AttemptID        string            `json:"attemptId,omitempty"` // the stream attempt that billed these tokens
	CacheDiagnostics *CacheDiagnostics `json:"cacheDiagnostics,omitempty"`
	// Session-cumulative cache tokens keep status displays steadier than one-turn values.
	SessionCacheHitTokens  int `json:"sessionCacheHitTokens"`
	SessionCacheMissTokens int `json:"sessionCacheMissTokens"`
	// Context* fields are the latest single-request shape for gauges/rebind.
	// When omitted, clients fall back to the billable prompt/completion totals.
	ContextPromptTokens     int     `json:"contextPromptTokens,omitempty"`
	ContextCompletionTokens int     `json:"contextCompletionTokens,omitempty"`
	ContextReasoningTokens  int     `json:"contextReasoningTokens,omitempty"`
	ContextCacheHitTokens   int     `json:"contextCacheHitTokens,omitempty"`
	ContextCacheMissTokens  int     `json:"contextCacheMissTokens,omitempty"`
	Cost                    float64 `json:"cost,omitempty"`
	Currency                string  `json:"currency,omitempty"`
	// CurrencyCode is the ISO code for Cost (preferred over symbol Currency).
	CurrencyCode string `json:"currencyCode,omitempty"`
	// CostUSD is a compatibility alias for older consumers; it mirrors Cost
	// (selected display valuation) and does not imply USD.
	CostUSD float64 `json:"costUsd,omitempty"`
	// CostQuote is the structured host-side quote. New consumers must prefer it
	// over cost/currency aliases. Never sent to model providers.
	CostQuote       *billing.CostQuote `json:"costQuote,omitempty"`
	CostComplete    bool               `json:"costComplete,omitempty"`
	DisplayComplete bool               `json:"displayComplete,omitempty"`
	DisplayStatus   string             `json:"displayStatus,omitempty"`
	AggregateMode   string             `json:"aggregateMode,omitempty"`
	OriginalTotals  []billing.Money    `json:"originalTotals,omitempty"`
}

// CacheDiagnostics is the JSON form of cache prefix diagnostics.
type CacheDiagnostics struct {
	PrefixHash          string   `json:"prefixHash"`
	PrefixChanged       bool     `json:"prefixChanged"`
	PrefixChangeReasons []string `json:"prefixChangeReasons,omitempty"`
	SystemHash          string   `json:"systemHash"`
	ToolsHash           string   `json:"toolsHash"`
	LogRewriteVersion   int      `json:"logRewriteVersion"`
	ToolSchemaTokens    int      `json:"toolSchemaTokens"`
	CacheMissTokens     int      `json:"cacheMissTokens"`
	CacheHitTokens      int      `json:"cacheHitTokens"`
	CarriedMessages     int      `json:"carriedMessages"`
	BodyChanged         bool     `json:"bodyChanged"`
	BodyHash            string   `json:"bodyHash"`
}

// Approval is the JSON form of an event.Approval.
type Approval struct {
	ID       string            `json:"id"`
	Tool     string            `json:"tool"`
	Subject  string            `json:"subject" externalizable:"true"`
	Reason   string            `json:"reason,omitempty" externalizable:"true"`
	Fresh    bool              `json:"fresh,omitempty"`
	Kind     string            `json:"kind,omitempty"` // tool | plan | recovery
	Recovery *RecoveryApproval `json:"recovery,omitempty"`
}

// RecoveryApproval is the JSON form of an event.RecoveryApproval.
type RecoveryApproval struct {
	SourceAgent     string `json:"source_agent,omitempty"`
	FailedTool      string `json:"failed_tool,omitempty"`
	FailedSummary   string `json:"failed_summary,omitempty"`
	Diagnosis       string `json:"diagnosis,omitempty"`
	NextTool        string `json:"next_tool,omitempty"`
	NextAction      string `json:"next_action,omitempty"`
	ChangeKind      string `json:"change_kind,omitempty"`
	ChangeRationale string `json:"change_rationale,omitempty"`
	ReviewRationale string `json:"review_rationale,omitempty"`
	PlanBefore      string `json:"plan_before,omitempty"`
	PlanAfter       string `json:"plan_after,omitempty"`
	CanGrantTask    bool   `json:"can_grant_task,omitempty"`
	TaskGrantScope  string `json:"task_grant_scope,omitempty"`
}

// Guardian is the JSON form of an event.GuardianResult.
type Guardian struct {
	ID                string `json:"id"`
	Tool              string `json:"tool"`
	Subject           string `json:"subject"`
	Outcome           string `json:"outcome"`
	RiskLevel         string `json:"risk_level,omitempty"`
	UserAuthorization string `json:"user_authorization,omitempty"`
	Rationale         string `json:"rationale,omitempty" externalizable:"true"`
	DurationMs        int64  `json:"duration_ms,omitempty"`
	Usage             *Usage `json:"usage,omitempty"`
}

// ToWireGuardian converts an event.GuardianResult into its JSON wire form.
func ToWireGuardian(g event.GuardianResult) *Guardian {
	out := &Guardian{
		ID:                g.ID,
		Tool:              g.Tool,
		Subject:           g.Subject,
		Outcome:           g.Outcome,
		RiskLevel:         g.RiskLevel,
		UserAuthorization: g.UserAuthorization,
		Rationale:         g.Rationale,
		DurationMs:        g.DurationMs,
	}
	if u := g.Usage; u != nil {
		out.Usage = &Usage{
			PromptTokens: u.PromptTokens, CompletionTokens: u.CompletionTokens,
			TotalTokens: u.TotalTokens, CacheHitTokens: u.CacheHitTokens,
			CacheMissTokens: u.CacheMissTokens, ReasoningTokens: u.ReasoningTokens,
			Estimated: u.Estimated,
		}
		if g.Pricing != nil {
			q := event.EnsureCostQuote(event.Event{Kind: event.Usage, Usage: u, Pricing: g.Pricing}, nil)
			if q != nil {
				out.Usage.CostQuote = q
				out.Usage.Cost = q.LegacyCostFloat()
				out.Usage.Currency = q.LegacyCurrencySymbol()
				out.Usage.CostUSD = out.Usage.Cost
				out.Usage.CurrencyCode = q.LegacyCurrencyCode()
			}
		}
	}
	return out
}

// ToWireAsk converts an event.Ask into its JSON wire form.
func ToWireAsk(a event.Ask) *Ask {
	qs := make([]AskQuestion, len(a.Questions))
	for i, q := range a.Questions {
		opts := make([]AskOption, len(q.Options))
		for j, o := range q.Options {
			opts[j] = AskOption{Label: o.Label, Description: o.Description}
		}
		qs[i] = AskQuestion{ID: q.ID, Header: q.Header, Prompt: q.Prompt, Reason: q.Reason, Options: opts, Multi: q.Multi}
	}
	return &Ask{ID: a.ID, Questions: qs}
}

// ToWireCacheDiagnostics converts cache diagnostics into their JSON wire form.
func ToWireCacheDiagnostics(d *event.CacheDiagnostics) *CacheDiagnostics {
	return &CacheDiagnostics{
		PrefixHash:          d.PrefixHash,
		PrefixChanged:       d.PrefixChanged,
		PrefixChangeReasons: append([]string(nil), d.PrefixChangeReasons...),
		SystemHash:          d.SystemHash,
		ToolsHash:           d.ToolsHash,
		LogRewriteVersion:   d.LogRewriteVersion,
		ToolSchemaTokens:    d.ToolSchemaTokens,
		CacheMissTokens:     d.CacheMissTokens,
		CacheHitTokens:      d.CacheHitTokens,
		CarriedMessages:     d.CarriedMessages,
		BodyChanged:         d.BodyChanged,
		BodyHash:            d.BodyHash,
	}
}

// KindNames returns every stable frontend event kind in event.Kind order. It is
// the protocol-neutral source used by consumers such as the Remote schema
// generator; callers receive a copy and may sort it without mutating eventwire.
func KindNames() []string {
	names := make([]string, 0, int(event.KindCount))
	for kind := range event.KindCount {
		if name, ok := kindNames[kind]; ok {
			names = append(names, name)
		}
	}
	return names
}

// KindName returns the stable wire name of one event kind, or false for a
// kind outside the known set.
func KindName(kind event.Kind) (string, bool) {
	name, ok := kindNames[kind]
	return name, ok
}

var kindNames = map[event.Kind]string{
	event.TurnStarted:             "turn_started",
	event.Reasoning:               "reasoning",
	event.Text:                    "text",
	event.Message:                 "message",
	event.ToolDispatch:            "tool_dispatch",
	event.ToolResult:              "tool_result",
	event.Usage:                   "usage",
	event.Notice:                  "notice",
	event.Phase:                   "phase",
	event.ApprovalRequest:         "approval_request",
	event.AskRequest:              "ask_request",
	event.TurnDone:                "turn_done",
	event.CompactionStarted:       "compaction_started",
	event.CompactionProgress:      "compaction_progress",
	event.CompactionDone:          "compaction_done",
	event.ToolProgress:            "tool_progress",
	event.MCPSurfaceReady:         "mcp_surface_ready",
	event.Retrying:                "retrying",
	event.Steer:                   "steer",
	event.GuardianAssessment:      "guardian_assessment",
	event.ExtensionSurface:        "extension_surface",
	event.ExtensionStatus:         "extension_status",
	event.StreamAttempt:           "stream_attempt",
	event.ContextMaintenanceEvent: "context_maintenance",
	event.TodoProgressEvent:       "todo_progress",
	event.WorkspaceChanged:        "workspace_changed",
	event.TurnPhase:               "turn_phase",
	event.CompletionSummary:       "completion_summary",
	event.AdjudicationsChanged:    "adjudications_changed",
	event.InboxChanged:            "inbox_changed",
	event.GraphDelta:              "graph_delta",
}

func toWireTodoProgress(p *event.TodoProgress) *TodoProgress {
	if p == nil {
		return nil
	}
	return &TodoProgress{
		Kind: p.Kind, Steps: p.Steps, Completed: p.Completed,
		ContentRevision: p.ContentRevision, PlanRevision: p.PlanRevision,
		ProgressRevision: p.ProgressRevision,
	}
}

// TodoProgress is the JSON form of event.TodoProgress.
type TodoProgress struct {
	Kind             string `json:"kind"`
	Steps            int    `json:"steps,omitempty"`
	Completed        int    `json:"completed,omitempty"`
	ContentRevision  int    `json:"contentRevision,omitempty"`
	PlanRevision     int    `json:"planRevision,omitempty"`
	ProgressRevision int    `json:"progressRevision,omitempty"`
}

// ContextMaintenance is the JSON form of event.ContextMaintenance.
type ContextMaintenance struct {
	Status              string `json:"status,omitempty"`
	Action              string `json:"action,omitempty"`
	Trigger             string `json:"trigger,omitempty"`
	OperationID         string `json:"operationId,omitempty"`
	InputTokens         int    `json:"inputTokens,omitempty"`
	ResultTokens        int    `json:"resultTokens,omitempty"`
	SavedTokens         int    `json:"savedTokens,omitempty"`
	AffectedToolResults int    `json:"affectedToolResults,omitempty"`
	ProjectionVersion   uint64 `json:"projectionVersion,omitempty"`
	CacheBreak          bool   `json:"cacheBreak,omitempty"`
	Reason              string `json:"reason,omitempty"`
	Code                string `json:"code,omitempty"`
	Boundary            string `json:"boundary,omitempty"`
	TriggerTokens       int    `json:"triggerTokens,omitempty"`
}
