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
	"reasonix/internal/usageledger"
)

type runOutputFormat string

const (
	runOutputText       runOutputFormat = "text"
	runOutputJSON       runOutputFormat = "json"
	runOutputStreamJSON runOutputFormat = "stream-json"
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

type runResultUsage = usageledger.Tokens

type runResult struct {
	SchemaVersion           int                               `json:"schema_version"`
	Type                    string                            `json:"type"`
	Subtype                 string                            `json:"subtype"`
	IsError                 bool                              `json:"is_error"`
	DurationMS              int64                             `json:"duration_ms"`
	NumTurns                int                               `json:"num_turns"`
	Result                  string                            `json:"result"`
	SessionID               string                            `json:"session_id,omitempty"`
	UsageIsIncomplete       bool                              `json:"usage_is_incomplete"`
	CostIsPartial           bool                              `json:"cost_is_partial"`
	TotalCostUSD            *float64                          `json:"total_cost_usd,omitempty"`
	TotalCostUSDTicks       *int64                            `json:"total_cost_usd_ticks,omitempty"`
	ModelUsage              map[string]usageledger.ModelUsage `json:"modelUsage"`
	IncompleteReasons       []string                          `json:"incomplete_reasons,omitempty"`
	OpenBackgroundSubagents int                               `json:"open_background_subagents,omitempty"`
	Usage                   runResultUsage                    `json:"usage"`
}

type runOutputSink struct {
	mu      sync.Mutex
	format  runOutputFormat
	out     io.Writer
	encoder *json.Encoder
	final   string
	ledger  *usageledger.Ledger
	turns   int
	err     error
}

func newRunOutputSink(out io.Writer, format runOutputFormat) *runOutputSink {
	return &runOutputSink{format: format, out: out, encoder: json.NewEncoder(out), ledger: usageledger.New()}
}

func (s *runOutputSink) Emit(e event.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e.Kind == event.Message {
		s.final = e.Text
	}
	s.ledger.Add(e)
	if e.Kind == event.TurnDone {
		s.turns++
	}
	if s.format == runOutputStreamJSON && s.err == nil {
		s.err = s.encoder.Encode(eventwire.ToWire(e))
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
	resultText := s.final
	subtype := "success"
	if runErr != nil {
		subtype = "error_during_execution"
		if resultText == "" {
			resultText = runErr.Error()
		}
	}
	turns := s.turns
	if turns == 0 && runErr == nil {
		turns = 1
	}
	projection := s.ledger.Projection()
	return s.encoder.Encode(runResult{
		SchemaVersion:           2,
		Type:                    "result",
		Subtype:                 subtype,
		IsError:                 runErr != nil,
		DurationMS:              time.Since(started).Milliseconds(),
		NumTurns:                turns,
		Result:                  resultText,
		SessionID:               sessionID,
		UsageIsIncomplete:       projection.UsageIsIncomplete,
		CostIsPartial:           projection.CostIsPartial,
		TotalCostUSD:            projection.TotalCostUSD,
		TotalCostUSDTicks:       projection.TotalCostUSDTicks,
		ModelUsage:              projection.ModelUsage,
		IncompleteReasons:       projection.IncompleteReasons,
		OpenBackgroundSubagents: projection.OpenBackgroundSubagents,
		Usage:                   projection.Usage,
	})
}
