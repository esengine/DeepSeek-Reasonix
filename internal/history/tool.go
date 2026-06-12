package history

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"reasonix/internal/tool"
)

type historyTool struct {
	searcher *Searcher
}

// NewTool returns a read-only history retrieval tool bound to local sessions.
func NewTool(opts Options) tool.Tool {
	return historyTool{searcher: NewSearcher(opts)}
}

func (historyTool) Name() string { return "history" }

func (historyTool) Description() string {
	return "Search saved local session history with lightweight BM25 retrieval, then read messages around a hit. " +
		"Use search when past decisions, failed attempts, commands, or tool inputs may help the current task; use around with a returned session_path and message_index to inspect the nearby transcript. " +
		"By default it searches user text, assistant text, tool inputs, and tool errors; normal tool outputs are excluded unless kind includes tool_output."
}

func (historyTool) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"operation": {"type": "string", "enum": ["search", "around"], "description": "search ranks saved history; around returns nearby messages for a search hit."},
			"query": {"type": "string", "description": "Search query for operation=search."},
			"scope": {"type": "string", "enum": ["project", "global"], "description": "project searches the current session directory; global also includes compacted-history archives."},
			"kind": {"type": "array", "items": {"type": "string", "enum": ["user_text", "assistant_text", "tool_input", "tool_error", "tool_output"]}, "description": "History parts to search. Defaults to user_text, assistant_text, tool_input, and tool_error."},
			"tool_name": {"type": "string", "description": "Optional tool-name filter for tool_input, tool_error, or tool_output."},
			"limit": {"type": "integer", "description": "Maximum search hits to return, default 8, max 20."},
			"session_path": {"type": "string", "description": "Path from a search hit. Required for operation=around."},
			"message_index": {"type": "integer", "description": "Message index from a search hit. Required for operation=around."},
			"before": {"type": "integer", "description": "Messages before message_index for operation=around, default 3, max 10."},
			"after": {"type": "integer", "description": "Messages after message_index for operation=around, default 3, max 10."}
		},
		"required": ["operation"]
	}`)
}

func (t historyTool) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var in struct {
		Operation    string `json:"operation"`
		Query        string `json:"query"`
		Scope        string `json:"scope"`
		Kind         []Kind `json:"kind"`
		ToolName     string `json:"tool_name"`
		Limit        int    `json:"limit"`
		SessionPath  string `json:"session_path"`
		MessageIndex *int   `json:"message_index"`
		Before       int    `json:"before"`
		After        int    `json:"after"`
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	switch strings.TrimSpace(in.Operation) {
	case "search":
		hits, err := t.searcher.Search(ctx, SearchRequest{
			Query:    in.Query,
			Scope:    in.Scope,
			Kinds:    in.Kind,
			ToolName: in.ToolName,
			Limit:    in.Limit,
		})
		if err != nil {
			return "", err
		}
		return formatHits(in.Query, hits), nil
	case "around":
		if in.MessageIndex == nil {
			return "", fmt.Errorf("message_index is required for operation=around")
		}
		msgs, err := t.searcher.Around(ctx, AroundRequest{
			SessionPath:  in.SessionPath,
			MessageIndex: *in.MessageIndex,
			Before:       in.Before,
			After:        in.After,
		})
		if err != nil {
			return "", err
		}
		return formatAround(in.SessionPath, *in.MessageIndex, msgs), nil
	case "":
		return "", fmt.Errorf("operation is required")
	default:
		return "", fmt.Errorf("unknown operation %q", in.Operation)
	}
}

func (historyTool) ReadOnly() bool { return true }

func formatHits(query string, hits []Hit) string {
	if len(hits) == 0 {
		return strings.Join([]string{
			"No saved session history matched " + strconvQuote(query) + ".",
			"",
			"0 results does not prove the event never happened. Try:",
			"1. Retry with fewer, rarer terms such as a function name, command, error phrase, ticket id, or decision keyword.",
			"2. Widen scope from project to global when cross-project or compacted-history context may matter.",
			"3. If you need tool output, include kind=[\"tool_output\"] or filter by tool_name for tool input/error/output searches.",
		}, "\n")
	}
	var b strings.Builder
	fmt.Fprintf(&b, "History search results for %s:\n", strconvQuote(query))
	for i, hit := range hits {
		fmt.Fprintf(&b, "\n%d. score=%.3f source=%s session_id=%s message_index=%d kind=%s role=%s",
			i+1, hit.Score, hit.Source, hit.SessionID, hit.MessageIndex, hit.Kind, hit.Role)
		if hit.ToolName != "" {
			fmt.Fprintf(&b, " tool=%s", hit.ToolName)
		}
		fmt.Fprintf(&b, "\n   session_path: %s\n   snippet: %s\n",
			hit.SessionPath, hit.Snippet)
	}
	b.WriteString("\nUse operation=\"around\" with a session_path and message_index to read nearby messages.")
	return strings.TrimSpace(b.String())
}

func formatAround(path string, idx int, msgs []MessageContext) string {
	var b strings.Builder
	fmt.Fprintf(&b, "History around %s message_index=%d:\n", path, idx)
	for _, msg := range msgs {
		fmt.Fprintf(&b, "\n%s\n", msg.Text)
	}
	return strings.TrimSpace(b.String())
}

func strconvQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
