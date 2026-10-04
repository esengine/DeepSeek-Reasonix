package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"reasonix/internal/contract/pricing"
	"strings"
	"sync"
	"time"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/eventwire"
)

type runOutputFormat string

const (
	runOutputText        runOutputFormat = "text"
	runOutputJSON        runOutputFormat = "json"
	runOutputStreamJSON  runOutputFormat = "stream-json"
	runOutputEventsJSONL runOutputFormat = "events-jsonl"
)

func parseRunOutputFormat(value string) (runOutputFormat, error) {
	switch runOutputFormat(strings.ToLower(strings.TrimSpace(value))) {
	case runOutputText:
		return runOutputText, nil
	case runOutputJSON:
		return runOutputJSON, nil
	case runOutputStreamJSON:
		return runOutputStreamJSON, nil
	default:
		return "", fmt.Errorf("unknown output format %q (want text, json, or stream-json)", value)
	}
}

// runOutputSessionID preserves the established json/stream-json contract while
// keeping the redacted events-jsonl surface independent from transcript names.
func runOutputSessionID(format runOutputFormat, rawSessionID string, identityKey []byte) string {
	if format == runOutputEventsJSONL {
		return machineSessionIDWithKey(rawSessionID, identityKey)
	}
	return rawSessionID
}

type runResultUsage struct {
	InputTokens              int  `json:"input_tokens"`
	OutputTokens             int  `json:"output_tokens"`
	CacheReadInputTokens     int  `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int  `json:"cache_creation_input_tokens"`
	Estimated                bool `json:"estimated,omitempty"`
}

// runPermissionRecord is what the result says about permissions: the posture
// the run settled on and every call its gate refused.
type runPermissionRecord struct {
	mode    string
	note    folderNote
	denials []runPermissionDenial
}

type runPermissionDenial struct {
	ToolName  string `json:"tool_name"`
	ToolUseID string `json:"tool_use_id"`
	Code      string `json:"code"`
	// Remedy is what the person does so this refusal stops, when the code has one.
	Remedy string `json:"remedy,omitempty"`
	Cause  string `json:"cause,omitempty"`
}

type runResult struct {
	Type       string  `json:"type"`
	Subtype    string  `json:"subtype"`
	IsError    bool    `json:"is_error"`
	DurationMS int64   `json:"duration_ms"`
	NumTurns   int     `json:"num_turns"`
	Result     string  `json:"result"`
	SessionID  string  `json:"session_id,omitempty"`
	TotalCost  float64 `json:"total_cost,omitempty"`
	Currency   string  `json:"currency,omitempty"`
	// TotalCostUSD is the released compatibility alias. It mirrors TotalCost;
	// new consumers must pair TotalCost with Currency instead of assuming USD.
	TotalCostUSD float64 `json:"total_cost_usd,omitempty"`
	// CostComplete is false when mixed originals lack a shared display valuation.
	CostComplete    bool   `json:"cost_complete"`
	DisplayComplete bool   `json:"display_complete"`
	DisplayStatus   string `json:"display_status,omitempty"`
	AggregateMode   string `json:"aggregate_mode,omitempty"`
	// OriginalCosts lists per-ISO original totals (never cross-added).
	OriginalCosts  map[string]float64 `json:"original_costs,omitempty"`
	OriginalTotals []pricing.Money    `json:"original_totals,omitempty"`
	CostQuote      *pricing.CostQuote `json:"cost_quote,omitempty"`
	Usage          runResultUsage     `json:"usage"`
	// PermissionDenials lists every call a permission gate refused, by the
	// refusal's code; a run that wrote nothing says why here, not only in prose.
	PermissionDenials []runPermissionDenial `json:"permission_denials"`
	// PermissionMode is the posture the run settled on, named or defaulted.
	PermissionMode string `json:"permission_mode,omitempty"`
	// Readiness is the unmet final-readiness judgement, if any; the run still
	// exits 0 unless --fail-on-unverified asked otherwise.
	Readiness *eventwire.FinalReadiness `json:"readiness,omitempty"`
	// UnverifiedBy is the refusal code that left the work undone when the run
	// still reports success; `--fail-on-unverified` turns it into exit 3.
	UnverifiedBy string                       `json:"unverified_by,omitempty"`
	Completion   *eventwire.CompletionSummary `json:"completion,omitempty"`
}

type machineEventUsage struct {
	InputTokens     int  `json:"input_tokens"`
	OutputTokens    int  `json:"output_tokens"`
	CacheHitTokens  int  `json:"cache_hit_tokens"`
	CacheMissTokens int  `json:"cache_miss_tokens"`
	Estimated       bool `json:"estimated,omitempty"`
}

// machineEventRecord is deliberately content-free. The existing stream-json
// format is a rich UI transport and includes prompts, tool arguments, results,
// and reasoning; this contract is for automation that must not receive them.
type machineEventRecord struct {
	SchemaVersion  int                `json:"schema_version"`
	Sequence       uint64             `json:"sequence"`
	Kind           string             `json:"kind"`
	Code           string             `json:"code,omitempty"`
	Level          string             `json:"level,omitempty"`
	ToolID         string             `json:"tool_id,omitempty"`
	ToolName       string             `json:"tool_name,omitempty"`
	ToolReadOnly   bool               `json:"tool_read_only,omitempty"`
	ToolError      bool               `json:"tool_error,omitempty"`
	RefusalCode    string             `json:"refusal_code,omitempty"`
	ToolTruncated  bool               `json:"tool_truncated,omitempty"`
	ToolDurationMS int64              `json:"tool_duration_ms,omitempty"`
	Usage          *machineEventUsage `json:"usage,omitempty"`
	ApprovalID     string             `json:"approval_id,omitempty"`
	ApprovalKind   string             `json:"approval_kind,omitempty"`
	AskID          string             `json:"ask_id,omitempty"`
	Outcome        string             `json:"outcome,omitempty"`
	Cancelled      bool               `json:"cancelled,omitempty"`
	Error          bool               `json:"error,omitempty"`
	RetryAttempt   int                `json:"retry_attempt,omitempty"`
	RetryMax       int                `json:"retry_max,omitempty"`
	CompactionType string             `json:"compaction_type,omitempty"`
	CompactionMsgs int                `json:"compaction_messages,omitempty"`
	GuardianResult string             `json:"guardian_result,omitempty"`
	GuardianRisk   string             `json:"guardian_risk,omitempty"`
}

type machineRunDone struct {
	SchemaVersion int               `json:"schema_version"`
	Sequence      uint64            `json:"sequence"`
	Kind          string            `json:"kind"`
	SessionID     string            `json:"session_id,omitempty"`
	OK            bool              `json:"ok"`
	DurationMS    int64             `json:"duration_ms"`
	NumTurns      int               `json:"num_turns"`
	Usage         machineEventUsage `json:"usage"`
	// PermissionDenials counts refused calls; the list itself carries content.
	PermissionDenials int                       `json:"permission_denials,omitempty"`
	Readiness         *eventwire.FinalReadiness `json:"readiness,omitempty"`
}

type runOutputSink struct {
	mu     sync.Mutex
	format runOutputFormat
	out    io.Writer
	// errOut takes what is about the run rather than its answer. Separate so a
	// caller piping stdout still sees warnings, and tests can capture them.
	errOut              io.Writer
	encoder             *json.Encoder
	final               string
	usage               runResultUsage
	cost                float64
	currency            string
	costComplete        bool
	displayComplete     bool
	displayStatus       string
	aggregateMode       string
	originalTotals      []pricing.Money
	sawQuote            bool
	originalCosts       map[string]float64
	verdict             runVerdict
	quoteLedger         *pricing.Ledger
	turns               int
	sequence            uint64
	machineToolIDs      map[string]string
	machineToolNames    map[string]string
	nextMachineToolID   uint64
	nextMachineToolName uint64
	permissions         runPermissionRecord
	turn                runTurnEnvelope
	err                 error
}

func newRunOutputSink(out io.Writer, format runOutputFormat) *runOutputSink {
	return &runOutputSink{
		format:           format,
		out:              out,
		errOut:           os.Stderr,
		encoder:          json.NewEncoder(out),
		machineToolIDs:   make(map[string]string),
		machineToolNames: make(map[string]string),
	}
}

func (s *runOutputSink) Emit(e event.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e.Kind == event.Message {
		s.final = e.Text
	}
	if e.Kind == event.Usage && e.Usage != nil {
		s.usage.InputTokens += e.Usage.PromptTokens
		s.usage.OutputTokens += e.Usage.CompletionTokens
		s.usage.CacheReadInputTokens += e.Usage.CacheHitTokens
		s.usage.CacheCreationInputTokens += e.Usage.CacheMissTokens
		s.usage.Estimated = s.usage.Estimated || e.Usage.Estimated
		q := e.CostQuote
		if q == nil && e.Pricing != nil {
			q = event.EnsureCostQuote(e, nil)
		}
		if q != nil {
			s.sawQuote = true
			if !q.CostComplete {
				s.costComplete = false
			}
			// First complete quote establishes complete=true.
			if q.Complete && s.quoteLedger == nil {
				s.costComplete = true
			}
			if s.originalCosts == nil {
				s.originalCosts = map[string]float64{}
			}
			if cur := pricing.NormalizeCurrency(q.Original.Currency); cur != "" {
				s.originalCosts[cur] += q.Original.Float64()
			}
			if q.Selected != nil && (s.currency == "" || s.currency == q.LegacyCurrencyCode()) {
				s.cost += q.Selected.Float64()
				s.currency = q.LegacyCurrencyCode()
			} else if q.Selected != nil {
				s.currency = ""
				s.cost = 0
			}
			if s.quoteLedger == nil {
				s.quoteLedger = pricing.NewLedger()
			}
			s.quoteLedger.Add(*q, pricing.UsageTokens{
				PromptTokens:           e.Usage.PromptTokens,
				CompletionTokens:       e.Usage.CompletionTokens,
				CacheHitTokens:         e.Usage.CacheHitTokens,
				CacheMissTokens:        e.Usage.CacheMissTokens,
				CacheWriteTokens:       e.Usage.CacheWriteTokens,
				CacheWriteBilledTokens: e.Usage.CacheWriteBilledTokens,
				Estimated:              e.Usage.Estimated,
			}, time.Now().UTC())
		} else if e.Pricing != nil {
			s.cost += e.Pricing.Cost(e.Usage)
			s.currency = pricingCurrencyCode(e.Pricing.Currency)
			s.costComplete = true
		}
	}
	if e.Kind == event.TurnDone {
		s.turns++
	}
	if d, ok := denialOf(e, s.permissions.note); ok {
		s.permissions.denials = append(s.permissions.denials, d)
	}
	s.verdict.observe(e)
	// stdout carries the answer alone, so a warning had nowhere to go and was
	// dropped — a planner fallback, a folded user turn, an unread check. stderr
	// already carries what the run says about itself and breaks no pipeline.
	if s.format == runOutputText && e.Kind == event.Notice && (e.Level == event.LevelWarn || closesWarning(e.Code)) {
		s.writeDiagnostic(e)
	}
	if s.format == runOutputStreamJSON || s.format == runOutputEventsJSONL {
		s.writeStreamEvent(e)
	}
}

// closesWarning names the info notices that end a warning already written to
// the diagnostic stream. Without them a headless run reports that it is
// blocked and never reports that it no longer is.
func closesWarning(code string) bool {
	return code == event.NoticeCodeWorkspaceLeaseResumed || code == event.NoticeCodeWorkspaceLeaseAbandoned
}

// writeDiagnostic reports one warning on the run's diagnostic stream, detail
// included, so the reason travels with the symptom.
func (s *runOutputSink) writeDiagnostic(e event.Event) {
	if s.errOut == nil {
		return
	}
	text := strings.TrimSpace(e.Text)
	if detail := strings.TrimSpace(e.Detail); detail != "" && detail != text && !event.DetailIsPayload(e.Code) {
		text = strings.TrimSpace(text + " " + detail)
	}
	if text == "" {
		return
	}
	fmt.Fprintf(s.errOut, "warning: %s\n", text)
}

func (s *runOutputSink) Finalize(sessionID string, started time.Time, runErr error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return s.err
	}
	// Mixed original currencies no longer error: totals use shared display
	// valuations when complete, otherwise cost_complete=false with original_costs.
	if s.format == runOutputText {
		if s.final != "" {
			_, s.err = fmt.Fprintln(s.out, s.final)
		}
		writeDenialWarning(s.errOut, s.permissions.denials)
		return s.err
	}
	completion := classifyRunCompletion(runErr)
	s.finishTurn(runErr, completion)
	if s.err != nil {
		return s.err
	}
	if s.format == runOutputEventsJSONL {
		s.sequence++
		turns := s.turns
		if turns == 0 && !completion.isError {
			turns = 1
		}
		return s.encoder.Encode(machineRunDone{
			SchemaVersion: machineSchemaVersion,
			Sequence:      s.sequence,
			Kind:          "run_done",
			SessionID:     sessionID,
			OK:            !completion.isError,
			DurationMS:    time.Since(started).Milliseconds(),
			NumTurns:      turns,
			Usage:         machineEventUsage{InputTokens: s.usage.InputTokens, OutputTokens: s.usage.OutputTokens, CacheHitTokens: s.usage.CacheReadInputTokens, CacheMissTokens: s.usage.CacheCreationInputTokens},

			PermissionDenials: len(s.permissions.denials),
			Readiness:         runReadiness(runErr),
		})
	}
	resultText := s.final
	if runErr != nil {
		if resultText == "" {
			resultText = runErr.Error()
		}
	}
	turns := s.turns
	if turns == 0 && !completion.isError {
		turns = 1
	}
	var aggQuote *pricing.CostQuote
	if s.quoteLedger != nil && len(s.quoteLedger.Entries) > 0 {
		agg := s.quoteLedger.Total("")
		aggQuote = &agg
		if agg.Selected != nil {
			s.cost = agg.Selected.Float64()
			s.currency = agg.LegacyCurrencyCode()
		}
		if agg.Selected == nil {
			s.cost = 0
			s.currency = ""
		}
		s.costComplete = agg.CostComplete
		s.displayComplete = agg.DisplayComplete
		s.displayStatus = agg.DisplayStatus
		s.aggregateMode = agg.AggregateMode
		if agg.OriginalTotals != nil {
			s.originalTotals = append([]pricing.Money(nil), agg.OriginalTotals...)
		}
	}
	return s.encoder.Encode(runResult{
		Type:              "result",
		Subtype:           completion.subtype,
		IsError:           completion.isError,
		DurationMS:        time.Since(started).Milliseconds(),
		NumTurns:          turns,
		Result:            resultText,
		SessionID:         sessionID,
		TotalCost:         s.cost,
		Currency:          s.currency,
		TotalCostUSD:      s.cost,
		CostComplete:      s.costComplete || (!s.sawQuote && s.currency != ""),
		DisplayComplete:   s.displayComplete,
		DisplayStatus:     s.displayStatus,
		AggregateMode:     s.aggregateMode,
		OriginalCosts:     s.originalCosts,
		OriginalTotals:    s.originalTotals,
		CostQuote:         aggQuote,
		Usage:             s.usage,
		PermissionDenials: append([]runPermissionDenial{}, s.permissions.denials...),
		PermissionMode:    s.permissions.mode,
		Readiness:         runReadiness(runErr),
		UnverifiedBy:      unverifiedBy(s.permissions.denials),
		Completion:        s.verdict.completion,
	})
}

func pricingCurrencyCode(value string) string {
	value = strings.TrimSpace(value)
	switch strings.ToUpper(value) {
	case "", "CNY", "RMB", "CNH", "¥", "￥":
		return "CNY"
	case "USD", "$", "US$":
		return "USD"
	default:
		if len(value) == 3 {
			return strings.ToUpper(value)
		}
		return value
	}
}

func (s *runOutputSink) machineEventRecordFor(e event.Event, sequence uint64) machineEventRecord {
	record := machineEventRecord{SchemaVersion: machineSchemaVersion, Sequence: sequence, Kind: machineEventKind(e.Kind)}
	switch e.Kind {
	case event.Notice:
		record.Code = e.Code
		if e.Level == event.LevelWarn {
			record.Level = "warn"
		} else {
			record.Level = "info"
		}
	case event.ToolDispatch, event.ToolResult, event.ToolProgress:
		// Tool-call IDs and names originate in the provider stream. Treat both as
		// untrusted content: an OpenAI-compatible endpoint may put arbitrary prompt
		// or argument text in either field. Per-run opaque aliases preserve event
		// correlation without exposing the provider-controlled values.
		record.ToolID = machineOpaqueValue(s.machineToolIDs, &s.nextMachineToolID, "tool", e.Tool.ID)
		record.ToolName = machineOpaqueValue(s.machineToolNames, &s.nextMachineToolName, "tool_name", e.Tool.Name)
		record.ToolReadOnly = e.Tool.ReadOnly
		record.ToolError = e.Tool.Err != ""
		record.RefusalCode = e.Tool.RefusalCode
		record.ToolTruncated = e.Tool.Bound.Lossy()
		record.ToolDurationMS = e.Tool.DurationMs
	case event.Usage:
		if e.Usage != nil {
			record.Usage = &machineEventUsage{InputTokens: e.Usage.PromptTokens, OutputTokens: e.Usage.CompletionTokens, CacheHitTokens: e.Usage.CacheHitTokens, CacheMissTokens: e.Usage.CacheMissTokens, Estimated: e.Usage.Estimated}
		}
	case event.ApprovalRequest:
		record.ApprovalID = e.Approval.ID
		record.ApprovalKind = e.Approval.Kind
	case event.AskRequest:
		record.AskID = e.Ask.ID
	case event.TurnDone:
		record.Outcome = e.Outcome
		record.Cancelled = e.Cancelled
		record.Error = e.Err != nil
	case event.CompactionStarted, event.CompactionDone:
		record.CompactionType = e.Compaction.Trigger
		record.CompactionMsgs = e.Compaction.Messages
	case event.GuardianAssessment:
		record.GuardianResult = e.Guardian.Outcome
		record.GuardianRisk = e.Guardian.RiskLevel
	case event.Retrying:
		record.RetryAttempt = e.RetryAttempt
		record.RetryMax = e.RetryMax
	}
	return record
}

func machineOpaqueValue(values map[string]string, next *uint64, prefix, value string) string {
	if value == "" {
		return ""
	}
	if opaque := values[value]; opaque != "" {
		return opaque
	}
	(*next)++
	opaque := fmt.Sprintf("%s_%d", prefix, *next)
	values[value] = opaque
	return opaque
}

func machineEventKind(kind event.Kind) string {
	names := eventwire.KindNames()
	if int(kind) >= 0 && int(kind) < len(names) {
		return names[kind]
	}
	return "unknown"
}

// setFolderNote says what a refusal for an untrusted folder tells the person.
func (s *runOutputSink) setFolderNote(n folderNote) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.permissions.note = n
	s.mu.Unlock()
}

// refusedByFolderTrust reports whether a refusal came from an untrusted folder.
func (s *runOutputSink) refusedByFolderTrust() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return refusedByFolderTrust(s.permissions.denials)
}

// SetPermissionMode records the posture the run settled on. A nil sink, which
// a text run without a result object has, takes nothing.
func (s *runOutputSink) SetPermissionMode(mode string) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.permissions.mode = mode
	s.mu.Unlock()
}
