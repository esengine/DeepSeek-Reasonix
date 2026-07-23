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

type runOutputSink struct {
	mu      sync.Mutex
	format  runOutputFormat
	out     io.Writer
	encoder *json.Encoder
	final   string
	usage   runResultUsage
	cost    float64
	turns   int
	err     error
}

func newRunOutputSink(out io.Writer, format runOutputFormat) *runOutputSink {
	return &runOutputSink{format: format, out: out, encoder: json.NewEncoder(out)}
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
	return s.encoder.Encode(runResult{
		Type:         "result",
		Subtype:      subtype,
		IsError:      runErr != nil,
		DurationMS:   time.Since(started).Milliseconds(),
		NumTurns:     turns,
		Result:       resultText,
		SessionID:    sessionID,
		TotalCostUSD: s.cost,
		Usage:        s.usage,
	})
}
