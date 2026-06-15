package acp

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"reasonix/internal/event"
	"reasonix/internal/permission"
	"reasonix/internal/provider"
)

// notifier is the slice of Conn the dispatch sink depends on: it pushes
// session/update notifications and, when a tool needs approval, makes a
// session/request_permission request. Narrowing to this interface keeps the sink
// unit-testable with a fake.
type notifier interface {
	Notify(method string, params any) error
	Request(ctx context.Context, method string, params any) (json.RawMessage, error)
}

// maxResultChars clips a tool result before it crosses the wire, matching main's
// dispatch.ts (the full result still goes to the model; this is display only).
const maxResultChars = 8000

// updateSink is an event.Sink bound to one session that maps the agent's typed
// event stream onto ACP session/update notifications. It is the v2 counterpart of
// main's dispatchKernelEvent: where main translated kernel events, we translate
// the event.Event the v2 agent already emits.
//
// v2 has no separate "tool intent" event — a call goes ToolDispatch → ToolResult,
// two states — so we emit a single pending tool_call on dispatch (already carrying
// rawInput, which main only had by the intent step) and a completed/failed
// tool_call_update on result. Message/Usage/Phase/TurnStarted/TurnDone have no
// place in main's update set and are dropped (TurnDone's outcome surfaces as the
// session/prompt stopReason instead).
//
// An ApprovalRequest is the controller asking the user to allow a gated tool
// call; the sink forwards it as a session/request_permission round-trip and feeds
// the answer back via approve (control.Controller.Approve), which the run loop is
// blocked on.
type updateSink struct {
	conn      notifier
	sessionID string
	approve   func(id string, allow, session, persist bool)
	mu        sync.Mutex
	turnCtx   context.Context
}

func newUpdateSink(conn notifier, sessionID string) *updateSink {
	return &updateSink{conn: conn, sessionID: sessionID}
}

// bindApprove installs the controller's Approve callback, called by the service
// once the controller exists (the sink is built first, to hand to the Factory).
func (s *updateSink) bindApprove(fn func(id string, allow, session, persist bool)) {
	if fn == nil {
		s.approve = nil
		return
	}
	s.approve = fn
}

func (s *updateSink) setTurnContext(ctx context.Context) {
	s.mu.Lock()
	s.turnCtx = ctx
	s.mu.Unlock()
}

func (s *updateSink) clearTurnContext() {
	s.mu.Lock()
	s.turnCtx = nil
	s.mu.Unlock()
}

func (s *updateSink) currentTurnContext() context.Context {
	s.mu.Lock()
	ctx := s.turnCtx
	s.mu.Unlock()
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

// Emit implements event.Sink. The agent calls it serially (see event.Sink), so no
// locking is needed; write serialization lives in Conn.
func (s *updateSink) Emit(e event.Event) {
	switch e.Kind {
	case event.Reasoning:
		if e.Text == "" {
			return
		}
		s.send(messageChunk{SessionUpdate: "agent_thought_chunk", Content: textBlock(e.Text)})

	case event.Text:
		if e.Text == "" {
			return
		}
		s.send(messageChunk{SessionUpdate: "agent_message_chunk", Content: textBlock(e.Text)})

	case event.ToolDispatch:
		// Skip the early (Partial) dispatch: it carries no args, and the full one
		// that follows is the single pending tool_call the protocol expects.
		if e.Tool.Partial {
			return
		}
		s.send(toolCall{
			SessionUpdate: "tool_call",
			ToolCallID:    e.Tool.ID,
			Title:         e.Tool.Name,
			Kind:          toolKindFor(e.Tool.Name),
			Status:        "pending",
			RawInput:      rawJSON(e.Tool.Args),
		})

	case event.ToolResult:
		status := "completed"
		text := e.Tool.Output
		if e.Tool.Err != "" {
			status = "failed"
			text = e.Tool.Err
		}
		s.send(toolCallUpdateMsg{
			SessionUpdate: "tool_call_update",
			ToolCallID:    e.Tool.ID,
			Status:        status,
			Content:       []toolContent{{Type: "content", Content: textBlock(clip(text))}},
		})

	case event.Notice:
		// Surface warnings to the host as a message chunk so they're not lost;
		// info-level notices stay out of band.
		if e.Level == event.LevelWarn && e.Text != "" {
			s.send(messageChunk{
				SessionUpdate: "agent_message_chunk",
				Content:       textBlock("\n\n[warning] " + e.Text),
			})
		}

	case event.CompactionDone:
		// ACP has no compaction-card concept; surface a one-line note so the host
		// knows the context was summarized (an aborted pass has no summary).
		if e.Compaction.Summary != "" {
			s.send(messageChunk{
				SessionUpdate: "agent_message_chunk",
				Content:       textBlock(fmt.Sprintf("\n\n[compacted %d earlier messages to save context]", e.Compaction.Messages)),
			})
		}

	case event.ApprovalRequest:
		// The run loop is now blocked awaiting Approve(id, …). Do the
		// client round-trip off the emit goroutine so Emit returns at once
		// (the agent emits serially); the answer unblocks the loop.
		turnCtx := s.currentTurnContext()
		go s.requestPermission(turnCtx, e.Approval)
	}
}

func (s *updateSink) send(update any) {
	_ = s.conn.Notify("session/update", SessionUpdateParams{SessionID: s.sessionID, Update: update})
}

// replay streams a loaded conversation back to the client as session/update
// notifications so a resumed session reconstructs its transcript view. The
// system message is skipped (not user-visible); everything is reported as already
// completed since it is history, not a live turn.
func (s *updateSink) replay(msgs []provider.Message) {
	for _, m := range msgs {
		switch m.Role {
		case provider.RoleUser:
			if m.Content != "" {
				s.send(messageChunk{SessionUpdate: "user_message_chunk", Content: textBlock(m.Content)})
			}
		case provider.RoleAssistant:
			if m.ReasoningContent != "" {
				s.send(messageChunk{SessionUpdate: "agent_thought_chunk", Content: textBlock(m.ReasoningContent)})
			}
			if m.Content != "" {
				s.send(messageChunk{SessionUpdate: "agent_message_chunk", Content: textBlock(m.Content)})
			}
			for _, tc := range m.ToolCalls {
				s.send(toolCall{
					SessionUpdate: "tool_call",
					ToolCallID:    tc.ID,
					Title:         tc.Name,
					Kind:          toolKindFor(tc.Name),
					Status:        "completed",
					RawInput:      rawJSON(tc.Arguments),
				})
			}
		case provider.RoleTool:
			s.send(toolCallUpdateMsg{
				SessionUpdate: "tool_call_update",
				ToolCallID:    m.ToolCallID,
				Status:        "completed",
				Content:       []toolContent{{Type: "content", Content: textBlock(clip(m.Content))}},
			})
		}
	}
}

// requestPermission forwards an approval request to the client as a
// session/request_permission round-trip and feeds the outcome back through
// approve. Any transport failure or a cancelled/rejected outcome denies the call,
// so the model gets a blocked result rather than the turn hanging.
func (s *updateSink) requestPermission(ctx context.Context, a event.Approval) {
	if s.approve == nil {
		return
	}
	title := a.Tool
	if a.Subject != "" {
		title = a.Tool + " " + a.Subject
	}
	options := approvalOptions(a.Tool, a.Subject)
	params := PermissionRequestParams{
		SessionID: s.sessionID,
		ToolCall: PermissionToolCall{
			ToolCallID: "gate-" + a.ID,
			Title:      title,
			Kind:       toolKindFor(a.Tool),
			Status:     "pending",
		},
		Options: options,
	}

	allow, session, persist := false, false, false
	if raw, err := s.conn.Request(ctx, "session/request_permission", params); err == nil {
		var res PermissionRequestResult
		if json.Unmarshal(raw, &res) == nil && res.Outcome.Outcome == "selected" {
			switch PermissionOptionKind(res.Outcome.OptionID) {
			case OptAllowOnce:
				allow = true
			case OptAllowAlways:
				allow, session = true, true
			case OptAllowPersistent:
				allow, session, persist = true, true, true
			}
		}
	}
	s.approve(a.ID, allow, session, persist)
}

func approvalOptionNames(tool, subject string) (session, persistent string) {
	sessionRule := permission.SessionGrantRuleForScope(tool, subject)
	persistentRule := permission.RememberRuleForScope(tool, subject)
	return "Allow " + sessionRule + " for this session", "Always allow " + persistentRule + " (save to config)"
}

func approvalOptions(tool, subject string) []PermissionOption {
	allowSessionName, allowPersistentName := approvalOptionNames(tool, subject)
	options := []PermissionOption{
		{OptionID: string(OptAllowOnce), Name: "Allow", Kind: OptAllowOnce},
		{OptionID: string(OptAllowAlways), Name: allowSessionName, Kind: OptAllowAlways},
		{OptionID: string(OptAllowPersistent), Name: allowPersistentName, Kind: OptAllowPersistent},
		{OptionID: string(OptRejectOnce), Name: "Reject", Kind: OptRejectOnce},
	}
	return options
}

// textBlock builds a text content block.
func textBlock(text string) ContentBlock { return ContentBlock{Type: "text", Text: text} }

// rawJSON returns args as a raw JSON value when it is valid JSON, else nil so the
// rawInput field is omitted rather than carrying a malformed payload.
func rawJSON(args string) json.RawMessage {
	if args == "" || !json.Valid([]byte(args)) {
		return nil
	}
	return json.RawMessage(args)
}

// clip truncates text to maxResultChars, appending a note, matching dispatch.ts.
func clip(text string) string {
	if len(text) <= maxResultChars {
		return text
	}
	end := maxResultChars
	for end > 0 && !utf8.ValidString(text[:end]) {
		end--
	}
	return text[:end] + "\n…(" +
		strconv.Itoa(len(text)-end) + " more chars truncated)"
}

// toolKindFor maps a tool name to the ACP tool kind the host uses to categorize
// the call in its UI. The kinds match main's restricted set
// (read/edit/search/execute/other). Known v2 built-ins map explicitly; anything
// else (plugins, the task tool) falls back to a name heuristic, then "other".
func toolKindFor(name string) string {
	switch name {
	case "read_file", "ls", "glob":
		return "read"
	case "grep":
		return "search"
	case "edit_file", "move_file", "multiedit", "write_file":
		return "edit"
	case "bash":
		return "execute"
	}
	n := strings.ToLower(name)
	switch {
	case strings.Contains(n, "search") || strings.Contains(n, "grep") || strings.Contains(n, "find"):
		return "search"
	case strings.Contains(n, "edit") || strings.Contains(n, "write") || strings.Contains(n, "replace"):
		return "edit"
	case strings.Contains(n, "read") || strings.Contains(n, "cat") || strings.Contains(n, "view"):
		return "read"
	case strings.Contains(n, "bash") || strings.Contains(n, "exec") || strings.Contains(n, "shell") || strings.Contains(n, "run"):
		return "execute"
	default:
		return "other"
	}
}
