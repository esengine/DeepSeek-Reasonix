package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"reasonix/internal/event"
	"reasonix/internal/eventwire"
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
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
}

type runResult struct {
	Type         string         `json:"type"`
	Subtype      string         `json:"subtype"`
	IsError      bool           `json:"is_error"`
	DurationMS   int64          `json:"duration_ms"`
	NumTurns     int            `json:"num_turns"`
	Result       string         `json:"result"`
	SessionID    string         `json:"session_id,omitempty"`
	TotalCostUSD float64        `json:"total_cost_usd"`
	Usage        runResultUsage `json:"usage"`
}

type machineEventUsage struct {
	InputTokens     int `json:"input_tokens"`
	OutputTokens    int `json:"output_tokens"`
	CacheHitTokens  int `json:"cache_hit_tokens"`
	CacheMissTokens int `json:"cache_miss_tokens"`
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
}

type runOutputSink struct {
	mu                  sync.Mutex
	format              runOutputFormat
	out                 io.Writer
	encoder             *json.Encoder
	final               string
	usage               runResultUsage
	cost                float64
	turns               int
	sequence            uint64
	machineToolIDs      map[string]string
	machineToolNames    map[string]string
	nextMachineToolID   uint64
	nextMachineToolName uint64
	err                 error
}

func newRunOutputSink(out io.Writer, format runOutputFormat) *runOutputSink {
	return &runOutputSink{
		format:           format,
		out:              out,
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
		if e.Pricing != nil {
			s.cost += e.Pricing.Cost(e.Usage)
		}
	}
	if e.Kind == event.TurnDone {
		s.turns++
	}
	if s.format == runOutputStreamJSON && s.err == nil {
		s.err = s.encoder.Encode(eventwire.ToWire(e))
	} else if s.format == runOutputEventsJSONL && s.err == nil {
		s.sequence++
		s.err = s.encoder.Encode(s.machineEventRecordFor(e, s.sequence))
	}
}

func (s *runOutputSink) Finalize(sessionID string, started time.Time, runErr error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return s.err
	}
	if s.format == runOutputText {
		if s.final != "" {
			_, s.err = fmt.Fprintln(s.out, s.final)
		}
		return s.err
	}
	completion := classifyRunCompletion(runErr)
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
	return s.encoder.Encode(runResult{
		Type:         "result",
		Subtype:      completion.subtype,
		IsError:      completion.isError,
		DurationMS:   time.Since(started).Milliseconds(),
		NumTurns:     turns,
		Result:       resultText,
		SessionID:    sessionID,
		TotalCostUSD: s.cost,
		Usage:        s.usage,
	})
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
		record.ToolTruncated = e.Tool.Truncated
		record.ToolDurationMS = e.Tool.DurationMs
	case event.Usage:
		if e.Usage != nil {
			record.Usage = &machineEventUsage{InputTokens: e.Usage.PromptTokens, OutputTokens: e.Usage.CompletionTokens, CacheHitTokens: e.Usage.CacheHitTokens, CacheMissTokens: e.Usage.CacheMissTokens}
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
