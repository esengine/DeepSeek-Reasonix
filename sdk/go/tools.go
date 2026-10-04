package extension

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"time"
)

// ToolFunc runs one call of a tool the extension serves. args is the model's
// arguments as raw JSON. A returned error reaches the model as the tool's own
// failure, which it can act on, rather than as a broken call.
type ToolFunc func(ctx context.Context, args json.RawMessage) (string, error)

// declareTools fills the handshake with the tools Options.Tools serves when
// the Handler named none itself.
func (s *server) declareTools(result *InitializeResult) {
	if len(result.Tools) > 0 || len(s.opts.Tools) == 0 {
		return
	}
	for name, run := range s.opts.Tools {
		if run != nil {
			result.Tools = append(result.Tools, name)
		}
	}
	slices.Sort(result.Tools)
}

func (s *server) handleToolCall(ctx context.Context, raw json.RawMessage) (any, error) {
	var p ToolCallParams
	if err := strictDecode(raw, &p); err != nil || strings.TrimSpace(p.Name) == "" || p.TimeoutMillis < 0 || !jsonKeyPresent(raw, "arguments") {
		return nil, MustProtocolError(ErrInvalidParams)
	}
	run, ok := s.opts.Tools[p.Name]
	if !ok || run == nil {
		return nil, MustProtocolError(ErrUnknownMethod)
	}
	if p.TimeoutMillis > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Duration(p.TimeoutMillis)*time.Millisecond)
		defer cancel()
	}
	content, err := run(ctx, p.Arguments)
	if err != nil {
		return ToolCallResult{Content: err.Error(), IsError: true}, nil
	}
	return ToolCallResult{Content: content}, nil
}
