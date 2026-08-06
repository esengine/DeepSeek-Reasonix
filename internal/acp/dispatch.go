package acp

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"unicode/utf8"

	"reasonix/internal/agent"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/eventwire"
	"reasonix/internal/permission"
	"reasonix/internal/provider"
	"reasonix/internal/shellparse"
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
	// cwd resolves relative tool-arg paths for tool_call locations. Set once
	// via bindCwd before the sink receives events.
	cwd     string
	approve func(id string, allow, session, persist bool)
	answer  func(id string, answers []event.AskAnswer)
	status  func(event.Event)
	// extensionSurface records the client's negotiated
	// reasonix.extensionSurface support: structured surfaces go out as vendor
	// session/update payloads on top of the always-sent text fallback.
	extensionSurface bool
	// speculativeToolIDs tracks parent-sampling tool IDs published under the
	// active stream_attempt (attempt-scoped partials only). Guarded by mu —
	// parent sampling and background sub-agents may Emit concurrently.
	speculativeToolIDs map[string]struct{}
	activeAttemptID    string
	mu                 sync.Mutex
	turnCtx            context.Context
}

func newUpdateSink(conn notifier, sessionID string) *updateSink {
	return &updateSink{conn: conn, sessionID: sessionID}
}

// bindCwd installs the session root used to absolutize tool_call locations.
func (s *updateSink) bindCwd(cwd string) { s.cwd = cwd }

// bindApprove installs the controller's Approve callback, called by the service
// once the controller exists (the sink is built first, to hand to the Factory).
func (s *updateSink) bindApprove(fn func(id string, allow, session, persist bool)) {
	if fn == nil {
		s.approve = nil
		return
	}
	s.approve = fn
}

// bindAnswer installs the controller's AnswerQuestion callback for AskRequest
// events.
func (s *updateSink) bindAnswer(fn func(id string, answers []event.AskAnswer)) {
	s.answer = fn
}

// bindStatus installs the vendor-status observer. It receives typed events,
// never raw reasoning text or terminal transcripts.
func (s *updateSink) bindStatus(fn func(event.Event)) { s.status = fn }

// bindExtensionSurface records whether the client negotiated structured
// extension-surface support in the initialize handshake.
func (s *updateSink) bindExtensionSurface(supported bool) { s.extensionSurface = supported }

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
	if s.status != nil {
		s.status(e)
	}
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

	case event.StreamAttempt:
		// Attempt bookkeeping only. ACP still skips partial ToolDispatch (no
		// pending card until full args arrive after commit), so discard must not
		// invent failures for unpublished IDs. Full dispatches and parentId
		// nested tools are real work and are never speculative.
		s.mu.Lock()
		switch e.StreamAttempt.Action {
		case event.StreamAttemptBegin:
			s.activeAttemptID = e.StreamAttempt.ID
			s.speculativeToolIDs = nil
		case event.StreamAttemptCommit, event.StreamAttemptDiscard:
			s.activeAttemptID = ""
			s.speculativeToolIDs = nil
		}
		s.mu.Unlock()

	case event.ToolDispatch:
		// Skip the early (Partial) dispatch and later same-ID preview refresh: ACP
		// expects one pending tool_call and has no file-diff update payload.
		if e.Tool.Partial || e.Tool.Refreshed {
			return
		}
		// Full dispatches only arrive after a committed sampling attempt (or from
		// nested sub-agents). Never mark them speculative.
		// todo_write is the agent's task list; mirror it as an ACP plan update so
		// the client renders structured progress alongside the tool_call.
		if e.Tool.Name == "todo_write" {
			if entries, ok := planEntriesFromTodoArgs(e.Tool.Args); ok {
				s.send(planUpdate{SessionUpdate: "plan", Entries: entries})
			}
		}
		s.send(toolCall{
			SessionUpdate: "tool_call",
			ToolCallID:    e.Tool.ID,
			Title:         e.Tool.Name,
			Kind:          toolKindFor(e.Tool.Name),
			Status:        "pending",
			RawInput:      rawJSON(e.Tool.Args),
			Locations:     s.toolLocations(e.Tool.Name, e.Tool.Args),
		})

	case event.ToolResult:
		status := "completed"
		text := e.Tool.Output
		if e.Tool.Err != "" {
			status = "failed"
			text = e.Tool.Err
		}
		if e.Tool.ID != "" {
			s.mu.Lock()
			delete(s.speculativeToolIDs, e.Tool.ID)
			s.mu.Unlock()
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

	case event.AskRequest:
		// ACP has no separate "ask the user a business question" method. Reuse
		// the standard permission round-trip with the question options as choices;
		// clients such as Zed already know how to render this interaction.
		turnCtx := s.currentTurnContext()
		go s.requestAsk(turnCtx, e.Ask)

	case event.ExtensionSurface, event.ExtensionStatus:
		s.emitExtension(e)
	}
}

// emitExtension maps one extension structured-UI event onto ACP updates. A
// client that negotiated reasonix.extensionSurface receives the structured DTO
// (the shared eventwire JSON contract) in a vendor session/update variant;
// every client — including that one, belt and suspenders — also receives the
// flattened text fallback as an ordinary agent_message_chunk. Blocking
// form/request prompts never arrive here: the hub routes those through
// AskRequest, which already rides the session/request_permission round-trip.
func (s *updateSink) emitExtension(e event.Event) {
	p := e.Extension
	if p == nil {
		return
	}
	if s.extensionSurface {
		if dto := eventwire.ToWireExtensionSurface(p); dto != nil {
			s.send(extensionSurfaceUpdate{
				SessionUpdate: extensionSurfaceUpdateKind,
				Meta: map[string]any{
					"reasonix.io": map[string]any{
						"extensionSurface": dto,
					},
				},
			})
		}
	}
	text := extensionSurfaceText(p)
	if text == "" {
		return
	}
	prefix := "\n\n"
	if extensionSeverityWarns(p) {
		prefix += "[warning] "
	}
	s.send(messageChunk{SessionUpdate: "agent_message_chunk", Content: textBlock(prefix + text)})
}

// extensionSurfaceText flattens one extension surface payload to plain text
// for clients without structured-surface support: status →
// "[plugin] label: detail", card → title + body + fields, form → title +
// message, notification → title + body.
func extensionSurfaceText(p *event.ExtensionSurfacePayload) string {
	var b strings.Builder
	write := func(s string) {
		if s == "" {
			return
		}
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString(s)
	}
	switch {
	case p.Status != nil:
		line := "[" + p.PluginID + "] " + p.Status.Label
		if p.Status.Detail != "" {
			line += ": " + p.Status.Detail
		}
		write(line)
	case p.Card != nil:
		write(p.Card.Title)
		body := p.Card.Text
		if p.Card.Markdown != "" {
			body = p.Card.Markdown
		}
		write(body)
		for _, f := range p.Card.Fields {
			write(f.Key + ": " + f.Value)
		}
	case p.Form != nil:
		write(p.Form.Title)
		write(p.Form.Message)
	case p.Notification != nil:
		write(p.Notification.Title)
		write(p.Notification.Body)
	}
	return b.String()
}

// extensionSeverityWarns reports whether the payload carries a warn/error
// severity, which earns the same "[warning] " prefix as event.Notice.
func extensionSeverityWarns(p *event.ExtensionSurfacePayload) bool {
	severity := ""
	if p.Status != nil {
		severity = p.Status.Severity
	}
	if p.Notification != nil {
		severity = p.Notification.Severity
	}
	return severity == "warn" || severity == "error"
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
			// Replay the user-authored view, not the persisted wire form:
			// UserMessageText strips injected transient blocks (<response-language>
			// etc.) and unwraps memory-compiler contracts, same as every other
			// surface (#6882). A turn that was pure injection replays as nothing.
			text := m.Content
			if steer, ok := agent.SteerText(text); ok {
				text = steer
			} else {
				text = agent.UserMessageText(m)
			}
			if text != "" {
				s.send(messageChunk{SessionUpdate: "user_message_chunk", Content: textBlock(text)})
			}
		case provider.RoleAssistant:
			if m.ReasoningContent != "" {
				s.send(messageChunk{SessionUpdate: "agent_thought_chunk", Content: textBlock(m.ReasoningContent)})
			}
			// Same display filter as live emission: goal markers and evidence
			// blocks stay in history for parsing but never reach the client.
			if display := agent.DisplayAssistantText(m.Content); display != "" {
				s.send(messageChunk{SessionUpdate: "agent_message_chunk", Content: textBlock(display)})
			}
			for _, tc := range m.ToolCalls {
				s.send(toolCall{
					SessionUpdate: "tool_call",
					ToolCallID:    tc.ID,
					Title:         tc.Name,
					Kind:          toolKindFor(tc.Name),
					Status:        "completed",
					RawInput:      rawJSON(tc.Arguments),
					Locations:     s.toolLocations(tc.Name, tc.Arguments),
				})
				// Replaying the latest plan keeps the client's plan view in sync
				// with the restored conversation; each update replaces the last.
				if tc.Name == "todo_write" {
					if entries, ok := planEntriesFromTodoArgs(tc.Arguments); ok {
						s.send(planUpdate{SessionUpdate: "plan", Entries: entries})
					}
				}
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
	options := approvalOptions(a.Tool, a.Subject, a.Fresh)
	params := PermissionRequestParams{
		SessionID: s.sessionID,
		ToolCall: PermissionToolCall{
			ToolCallID: "gate-" + a.ID,
			Title:      title,
			Kind:       toolKindFor(a.Tool),
			Status:     "pending",
			RawInput:   rawJSON(string(a.RawInput)),
			Locations:  s.toolLocations(a.Tool, string(a.RawInput)),
			Meta:       s.permissionMeta(a),
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
			}
		}
	}
	s.approve(a.ID, allow, session, persist)
}

// permissionMeta carries Reasonix-owned structured data that an ACP supervisor
// may trust independently from model-supplied rawInput. A foreground bash call
// receives argv only when the command is a single static command: shell
// expansion, control operators, redirects, assignments, and background jobs all
// fail closed and remain interactive.
func (s *updateSink) permissionMeta(a event.Approval) map[string]any {
	reasonix := map[string]any{
		"approvalId": a.ID,
		"tool":       a.Tool,
		"subject":    a.Subject,
		"fresh":      a.Fresh,
	}
	if reason := strings.TrimSpace(a.Reason); reason != "" {
		reasonix["reason"] = reason
	}
	if a.Tool == "bash" && strings.TrimSpace(s.cwd) != "" {
		var input struct {
			Command                     string `json:"command"`
			RunInBackground             bool   `json:"run_in_background"`
			PreserveBackgroundProcesses bool   `json:"preserve_background_processes"`
		}
		if json.Unmarshal(a.RawInput, &input) == nil &&
			!input.RunInBackground && !input.PreserveBackgroundProcesses {
			cwd, cwdErr := filepath.Abs(s.cwd)
			features, featureOK := shellparse.AnalyzeApprovalFeatures(input.Command)
			command, commandErr := shellparse.ParseStaticCommand(input.Command, shellparse.StaticCommandPolicy{})
			exact := featureOK && !features.DynamicCommandName && !features.NestedExecution &&
				!features.Expansion && !features.Assignment && !features.Redirection &&
				!shellparse.ContainsUnquotedGlob(input.Command)
			for _, arg := range command.Argv {
				// Tilde expansion is shell-dependent and therefore not exact argv.
				if strings.HasPrefix(arg, "~") {
					exact = false
				}
			}
			if cwdErr == nil && commandErr == nil && exact && len(command.Argv) > 0 {
				reasonix["commandSchemaVersion"] = 1
				reasonix["argv"] = command.Argv
				reasonix["cwd"] = cwd
			}
		}
	}
	return map[string]any{"reasonix.io": reasonix}
}

func (s *updateSink) requestAsk(ctx context.Context, a event.Ask) {
	if s.answer == nil {
		return
	}
	answers := make([]event.AskAnswer, 0, len(a.Questions))
	for _, q := range a.Questions {
		selected, ok := s.requestAskQuestion(ctx, a.ID, q)
		if !ok {
			s.answer(a.ID, nil)
			return
		}
		answers = append(answers, event.AskAnswer{QuestionID: q.ID, Selected: []string{selected}})
	}
	s.answer(a.ID, answers)
}

func (s *updateSink) requestAskQuestion(ctx context.Context, askID string, q event.AskQuestion) (string, bool) {
	title := strings.TrimSpace(q.Prompt)
	if title == "" {
		title = strings.TrimSpace(q.Header)
	}
	if title == "" {
		title = "Question"
	}
	content := []toolContent(nil)
	if q.Header != "" && q.Header != title {
		content = append(content, toolContent{Type: "content", Content: textBlock(q.Header)})
	}
	options := make([]PermissionOption, 0, len(q.Options)+1)
	labelsByID := make(map[string]string, len(q.Options))
	for i, opt := range q.Options {
		id := fmt.Sprintf("%s:%d", q.ID, i+1)
		name := strings.TrimSpace(opt.Label)
		if strings.TrimSpace(opt.Description) != "" {
			name += " - " + strings.TrimSpace(opt.Description)
		}
		options = append(options, PermissionOption{OptionID: id, Name: name, Kind: OptAllowOnce})
		labelsByID[id] = opt.Label
	}
	options = append(options, PermissionOption{OptionID: q.ID + ":cancel", Name: "Cancel", Kind: OptRejectOnce})

	rawInput, _ := json.Marshal(map[string]any{
		"id":       q.ID,
		"question": title,
		"options":  q.Options,
		"multi":    q.Multi,
	})
	params := PermissionRequestParams{
		SessionID: s.sessionID,
		ToolCall: PermissionToolCall{
			ToolCallID: "ask-" + askID + "-" + q.ID,
			Title:      title,
			Kind:       "other",
			Status:     "pending",
			Content:    content,
			RawInput:   rawInput,
		},
		Options: options,
	}

	raw, err := s.conn.Request(ctx, "session/request_permission", params)
	if err != nil {
		return "", false
	}
	var res PermissionRequestResult
	if json.Unmarshal(raw, &res) != nil || res.Outcome.Outcome != "selected" {
		return "", false
	}
	label, ok := labelsByID[res.Outcome.OptionID]
	return label, ok
}

func approvalSessionOptionName(tool, subject string) string {
	if tool == control.SandboxEscapeApprovalTool {
		return "Use real environment for this session"
	}
	sessionRule := permission.SessionGrantRuleForScope(tool, subject)
	return "Allow " + sessionRule + " for this session"
}

func approvalOptions(tool, subject string, fresh bool) []PermissionOption {
	if fresh || control.RequiresFreshHumanApprovalTool(tool) {
		if tool == control.SandboxEscapeApprovalTool {
			return []PermissionOption{
				{OptionID: string(OptAllowOnce), Name: "Allow", Kind: OptAllowOnce},
				{OptionID: string(OptAllowAlways), Name: approvalSessionOptionName(tool, subject), Kind: OptAllowAlways},
				{OptionID: string(OptRejectOnce), Name: "Reject", Kind: OptRejectOnce},
			}
		}
		return []PermissionOption{
			{OptionID: string(OptAllowOnce), Name: "Allow", Kind: OptAllowOnce},
			{OptionID: string(OptRejectOnce), Name: "Reject", Kind: OptRejectOnce},
		}
	}
	allowSessionName := approvalSessionOptionName(tool, subject)
	options := []PermissionOption{
		{OptionID: string(OptAllowOnce), Name: "Allow", Kind: OptAllowOnce},
		{OptionID: string(OptAllowAlways), Name: allowSessionName, Kind: OptAllowAlways},
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
	case control.SandboxEscapeApprovalTool:
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

// locationTools names the builtin tools whose "path" argument is a real file
// target worth a follow-along location. Search/list tools are excluded: their
// path is a directory scope, not a file the user would want opened.
var locationTools = map[string]bool{
	"read_file":     true,
	"write_file":    true,
	"edit_file":     true,
	"multi_edit":    true,
	"notebook_edit": true,
	"delete_range":  true,
	"delete_symbol": true,
	"code_index":    true,
}

// toolLocations derives the file location a tool call touches from its raw
// args, so the client can follow along in the editor. Unknown tools and
// path-less args yield nil.
func (s *updateSink) toolLocations(name, rawArgs string) []ToolCallLocation {
	if !locationTools[name] {
		return nil
	}
	var p struct {
		Path   string `json:"path"`
		Offset int    `json:"offset"`
	}
	if json.Unmarshal([]byte(rawArgs), &p) != nil || strings.TrimSpace(p.Path) == "" {
		return nil
	}
	loc := ToolCallLocation{Path: s.absPath(p.Path)}
	// read_file's offset is a 0-based start line; surface it so the editor can
	// jump to the region being read.
	if name == "read_file" && p.Offset > 0 {
		line := p.Offset + 1
		loc.Line = &line
	}
	return []ToolCallLocation{loc}
}

func (s *updateSink) absPath(p string) string {
	if filepath.IsAbs(p) || s.cwd == "" {
		return p
	}
	return filepath.Join(s.cwd, p)
}

// planEntriesFromTodoArgs maps a todo_write argument payload onto ACP plan
// entries. Phase items (level 0) rank high, sub-steps medium; unknown statuses
// degrade to pending so a malformed item cannot poison the whole update.
func planEntriesFromTodoArgs(rawArgs string) ([]PlanEntry, bool) {
	var p struct {
		Todos []struct {
			Content string `json:"content"`
			Status  string `json:"status"`
			Level   int    `json:"level"`
		} `json:"todos"`
	}
	if json.Unmarshal([]byte(rawArgs), &p) != nil || len(p.Todos) == 0 {
		return nil, false
	}
	entries := make([]PlanEntry, 0, len(p.Todos))
	for _, t := range p.Todos {
		if strings.TrimSpace(t.Content) == "" {
			continue
		}
		status := t.Status
		switch status {
		case "pending", "in_progress", "completed":
		default:
			status = "pending"
		}
		priority := "medium"
		if t.Level == 0 {
			priority = "high"
		}
		entries = append(entries, PlanEntry{Content: t.Content, Priority: priority, Status: status})
	}
	if len(entries) == 0 {
		return nil, false
	}
	return entries, true
}
