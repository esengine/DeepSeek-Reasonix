package provider

import (
	"encoding/json"
	"strings"
)

// interruptedToolResult stands in for a tool result that never landed — an
// assistant tool_calls turn whose execution was cut short (interrupt, crash) and
// later resumed. Sending such a turn unanswered trips the OpenAI/DeepSeek 400
// "An assistant message with 'tool_calls' must be followed by tool messages
// responding to each 'tool_call_id'".
const interruptedToolResult = "[no result: the previous turn was interrupted before this tool call completed]"

// SanitizeOp is one step in the message sanitization pipeline. Each step
// receives the full message slice and returns a (possibly modified) copy.
// Steps are applied in order by ApplySanitize before sending to any provider.
type SanitizeOp func(msgs []Message) []Message

// DefaultSanitize is the ordered pipeline applied before sending to any provider.
// Add new sanitize steps here as data-format issues are discovered.
var DefaultSanitize = []SanitizeOp{
	sanitizeToolPairing,
}

// ApplySanitize runs the full sanitization pipeline on msgs and returns the
// result. The original slice is never mutated. Providers call this before
// building their wire-format request.
func ApplySanitize(msgs []Message) []Message {
	for _, op := range DefaultSanitize {
		msgs = op(msgs)
	}
	return msgs
}

// SanitizeToolPairing is the public alias for ApplySanitize, kept for backward
// compatibility with existing callers and tests.
func SanitizeToolPairing(msgs []Message) []Message {
	return ApplySanitize(msgs)
}

// sanitizeToolPairing is the core pipeline step: it repairs a history so it
// satisfies the tool-call contract the OpenAI-compatible and Anthropic APIs
// enforce — every assistant tool_calls entry must be answered by a following
// tool message for its id, and a tool message must follow such a call. It
// backfills a placeholder result for any unanswered call (so the turn stays
// intact), drops orphan tool messages, and closes truncated call-argument JSON
// (DeepSeek 400s on replayed half-streamed args, #3953).
// Well-formed histories pass through unchanged (results stay in call order).
// Callers send the result; the stored session keeps the original.
func sanitizeToolPairing(msgs []Message) []Message {
	out := make([]Message, 0, len(msgs))
	for i := 0; i < len(msgs); {
		m := msgs[i]
		if m.Role == RoleAssistant && len(m.ToolCalls) > 0 {
			j := i + 1
			for j < len(msgs) && msgs[j].Role == RoleTool {
				j++
			}
			// Backfill empty tool-call names from the corresponding tool
			// results so the model sees which tool was invoked (#4727).
			// The wire-format fix (openai.go) ensures empty fields are
			// never omitted, so this backfill is a UX improvement, not a
			// correctness requirement.
			calls := backfillToolCallNames(m.ToolCalls, msgs[i+1:j])
			m.ToolCalls = calls
			out = append(out, repairToolCallArgs(m))
			out = append(out, pairToolResults(calls, msgs[i+1:j])...)
			i = j
			continue
		}
		if m.Role == RoleTool {
			i++ // orphan tool message (no preceding assistant tool_calls) — drop
			continue
		}
		out = append(out, m)
		i++
	}
	return out
}

// repairToolCallArgs returns m with any undecodable tool-call Arguments closed
// into valid JSON (copy-on-write; the caller's history is never mutated). Empty
// arguments pass through — some gateways send "" for no-arg tools.
func repairToolCallArgs(m Message) Message {
	broken := false
	for _, tc := range m.ToolCalls {
		if tc.Arguments != "" && !json.Valid([]byte(tc.Arguments)) {
			broken = true
			break
		}
	}
	if !broken {
		return m
	}
	calls := make([]ToolCall, len(m.ToolCalls))
	copy(calls, m.ToolCalls)
	for i := range calls {
		if calls[i].Arguments == "" || json.Valid([]byte(calls[i].Arguments)) {
			continue
		}
		calls[i].Arguments = closeTruncatedJSON(calls[i].Arguments)
	}
	m.ToolCalls = calls
	return m
}

// closeTruncatedJSON best-effort completes a JSON document cut off mid-stream
// (unterminated string, open braces, dangling comma/colon); anything still
// invalid after closing degrades to "{}".
func closeTruncatedJSON(s string) string {
	var stack []byte
	inStr, esc := false, false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inStr {
			switch {
			case esc:
				esc = false
			case c == '\\':
				esc = true
			case c == '"':
				inStr = false
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
		case '{':
			stack = append(stack, '}')
		case '[':
			stack = append(stack, ']')
		case '}', ']':
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
		}
	}
	out := s
	if esc {
		out = out[:len(out)-1]
	}
	if inStr {
		out += `"`
	}
	trimmed := strings.TrimRight(out, " \t\r\n")
	switch {
	case strings.HasSuffix(trimmed, ","):
		out = trimmed[:len(trimmed)-1]
	case strings.HasSuffix(trimmed, ":"):
		out = trimmed + "null"
	}
	for i := len(stack) - 1; i >= 0; i-- {
		out += string(stack[i])
	}
	if !json.Valid([]byte(out)) {
		return "{}"
	}
	return out
}

// pairToolResults answers each tool_call with its result, backfilling a
// placeholder for any unanswered one. Distinct non-empty ids pair by id (so
// reordered results re-sort to call order); empty or duplicate ids pair by
// position instead — some gateways stream tool calls by index with no id, and a
// map keyed on id would collapse those results into one (call order is preserved
// because the loop appends results in call order).
func pairToolResults(calls []ToolCall, avail []Message) []Message {
	out := make([]Message, 0, len(calls))
	if idDistinct(calls) {
		byID := make(map[string]Message, len(avail))
		for _, r := range avail {
			byID[r.ToolCallID] = r
		}
		for _, tc := range calls {
			if r, ok := byID[tc.ID]; ok {
				out = append(out, r)
			} else {
				out = append(out, Message{Role: RoleTool, ToolCallID: tc.ID, Name: tc.Name, Content: interruptedToolResult})
			}
		}
		return out
	}
	for k, tc := range calls {
		if k < len(avail) {
			r := avail[k]
			r.ToolCallID = tc.ID
			out = append(out, r)
		} else {
			out = append(out, Message{Role: RoleTool, ToolCallID: tc.ID, Name: tc.Name, Content: interruptedToolResult})
		}
	}
	return out
}

// backfillToolCallNames returns calls with any empty Name filled in from the
// matching tool result (by id, then by position). Old sessions (#4727) may have
// saved assistant tool-calls with an empty name; backfilling gives the model
// useful context during replay. The common case (no empty names) returns the
// input unchanged without allocating. Unpaired calls keep their empty name,
// which the wire-format fix (openai.go) handles gracefully.
func backfillToolCallNames(calls []ToolCall, results []Message) []ToolCall {
	missing := false
	for _, c := range calls {
		if c.Name == "" {
			missing = true
			break
		}
	}
	if !missing {
		return calls
	}
	out := make([]ToolCall, len(calls))
	copy(out, calls)
	if idDistinct(calls) {
		byID := make(map[string]string, len(results))
		for _, r := range results {
			if r.Name != "" {
				byID[r.ToolCallID] = r.Name
			}
		}
		for k := range out {
			if out[k].Name == "" {
				if n, ok := byID[out[k].ID]; ok {
					out[k].Name = n
				}
			}
		}
		return out
	}
	// Fallback: positional pairing (same order as pairToolResults).
	for k := range out {
		if out[k].Name == "" && k < len(results) {
			out[k].Name = results[k].Name
		}
	}
	return out
}

// idDistinct reports whether every call carries a non-empty id unique within the
// batch — the condition under which id-keyed pairing is safe.
func idDistinct(calls []ToolCall) bool {
	seen := make(map[string]struct{}, len(calls))
	for _, tc := range calls {
		if tc.ID == "" {
			return false
		}
		if _, dup := seen[tc.ID]; dup {
			return false
		}
		seen[tc.ID] = struct{}{}
	}
	return true
}
