package serve

import (
	"net/http"
	"reasonix/internal/base/backgroundwork"
	"reasonix/internal/state/sessionstore"
	"strconv"
	"unicode/utf8"

	"reasonix/internal/base/diff"
	"reasonix/internal/contract/delivery"
	"reasonix/internal/contract/event"
	"reasonix/internal/contract/eventwire"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/surfacespec"
	"reasonix/internal/contract/tool"
)

type historyToolCall struct {
	ID                    string                          `json:"id"`
	Name                  string                          `json:"name"`
	Arguments             string                          `json:"arguments"`
	ArgumentNormalization *provider.ArgumentNormalization `json:"argumentNormalization,omitempty"`
	// What a stable proxy actually reached. Name stays the provider-visible
	// call, so a rebuild without these draws use_capability where the live
	// transcript drew the capability it resolved to.
	ResolvedName string `json:"resolvedName,omitempty"`
	CapabilityID string `json:"capabilityId,omitempty"`
}

type historyMessage struct {
	Deliveries []delivery.File `json:"deliveries,omitempty"`
	Role       string          `json:"role"`
	Content    string          `json:"content"`
	Reasoning  string          `json:"reasoning,omitempty"`
	// Images is how many attachments the turn carried, not the attachments
	// themselves: a turn that was only an image has no text to rebuild from,
	// and a reader that sees zero of both drops it as host chrome.
	Images int `json:"images,omitempty"`
	// Attachments are the stored files the message references, resolved by the
	// host so a reopened transcript previews what the live one did.
	Attachments []eventwire.Attachment `json:"attachments,omitempty"`
	// The session index this message occupies, which is what a checkpoint's
	// boundary names. A reader rebuilding a transcript joins on it rather than
	// on where a row happened to land after host chrome was dropped.
	MsgIndex int `json:"msgIndex"`
	// HostAuthored marks a user-role message the host wrote. It is the writer's
	// own declaration, so a reader never has to decide from the wording whether
	// a line was typed by the person or injected on their behalf.
	HostAuthored bool `json:"hostAuthored,omitempty"`
	// Guidance sent into a turn already running, rather than a turn of its own.
	// It has no checkpoint behind it, so nothing can be rewound to it.
	Steer bool `json:"steer,omitempty"`
	// Via is the paired device a user message was sent from; absent is the
	// window. Written when the message landed, so a reload still says it.
	Via *eventwire.Via `json:"via,omitempty"`
	// Which model wrote this assistant turn. A reopened transcript has no
	// turn_started to read it off, and the composer's current setting is a
	// different fact that has usually moved on.
	ModelRef  string `json:"modelRef,omitempty"`
	ThoughtMs int64  `json:"thoughtMs,omitempty"` // host-measured, so a reopened turn keeps it
	// CreatedAt is when a user message landed (unix ms); on the one that opens a
	// turn it is the turn's start.
	CreatedAt int64 `json:"createdAt,omitempty"`
	// WorkDurationMs is the turn's run time, set only on the message that closed it.
	WorkDurationMs int64             `json:"workDurationMs,omitempty"`
	ToolCalls      []historyToolCall `json:"toolCalls,omitempty"`
	ToolCallID     string            `json:"toolCallId,omitempty"`
	// The host's own account of a result that did not succeed. Without it a
	// rebuilt card has only the words, which a tool's own output can imitate.
	ToolFailed      bool   `json:"toolFailed,omitempty"`
	ToolRefusalCode string `json:"toolRefusalCode,omitempty"`
	ToolName        string `json:"toolName,omitempty"`
	// OutputDiff marks a rebuilt shell result whose whole output is a unified
	// diff, so a reopened session renders it the way the live sink did. It is
	// re-derived here because the event stream is live-only.
	OutputDiff bool `json:"outputDiff,omitempty"`
}

func historyMessages(msgs []provider.Message) []historyMessage {
	return historyMessagesWith(msgs, nil)
}

// historyMessagesWith also names the stored attachments each user message
// references, through the host's resolver.
func historyMessagesWith(msgs []provider.Message, attachments func(string) []event.Attachment) []historyMessage {
	return historyMessageWindow(msgs, attachments, 0, sessionstore.TurnWorkMs(msgs), make(map[string]provider.ToolCall))
}

func historyMessageWindow(msgs []provider.Message, attachments func(string) []event.Attachment, offset int, worked []int64, calls map[string]provider.ToolCall) []historyMessage {
	attached := func(text string) []eventwire.Attachment {
		if attachments == nil {
			return nil
		}
		return eventwire.ToWireAttachments(attachments(text))
	}
	out := make([]historyMessage, 0, len(msgs))
	for i, m := range msgs {
		if m.LocalOnly && m.Name == surfacespec.ActionMessageName {
			continue
		}
		// A steer is the person speaking into a running turn, so it stays a
		// user message and carries what it was. The host's own mid-turn notice
		// rides the same prefix and is not theirs to have said.
		if m.Role == provider.RoleUser {
			if steerText, host, isSteer := sessionstore.SteerKind(m.Content); isSteer {
				if m.RawContent != "" {
					steerText = m.RawContent
				}
				out = append(out, historyMessage{
					Role: string(provider.RoleUser), Content: steerText, Steer: true,
					HostAuthored: host || m.HostAuthored, MsgIndex: offset + i, Via: eventwire.ToWireVia(m.Via),
				})
				if !host && !m.HostAuthored {
					out[len(out)-1].Attachments = attached(steerText)
				}
				continue
			}
		}
		hm := historyMessage{Role: string(m.Role), Content: m.Content, MsgIndex: offset + i, HostAuthored: m.HostAuthored}
		if m.Role == provider.RoleUser {
			// Content is what the model saw, and one @-reference expands into a
			// whole file. A reopened session has to show what was typed.
			hm.Content = sessionstore.UserMessageText(m)
			hm.Images = len(m.Images)
			hm.Via = eventwire.ToWireVia(m.Via)
			hm.CreatedAt = m.CreatedAt
			if !m.HostAuthored {
				hm.Attachments = attached(hm.Content)
			}
		}
		if m.Role == provider.RoleAssistant {
			hm.ModelRef = m.ModelRef
			hm.Reasoning = m.ReasoningContent
			hm.ThoughtMs = m.ThoughtMs
			hm.WorkDurationMs = worked[i]
			if len(m.ToolCalls) > 0 {
				hm.ToolCalls = make([]historyToolCall, len(m.ToolCalls))
				for i, tc := range m.ToolCalls {
					calls[tc.ID] = tc
					hm.ToolCalls[i] = projectHistoryToolCall(tc)
				}
			}
		}
		if m.Role == provider.RoleTool {
			call := calls[m.ToolCallID]
			manifest := m.Content
			if m.RawContent != "" {
				manifest = m.RawContent
			}
			hm.Deliveries = delivery.Decode(call.ResolvedName, call.CapabilityID, m.ToolFailure != nil, manifest)
			hm.ToolCallID = m.ToolCallID
			hm.ToolName = m.Name
			if m.ToolFailure != nil {
				hm.ToolFailed = true
				hm.ToolRefusalCode = m.ToolFailure.RefusalCode
			}
		}
		out = append(out, hm)
	}
	return out
}

// history returns the session's message log so a reconnecting client can
// repopulate its transcript, including historical tool cards. Supports ETag caching:
// if the client sends If-None-Match with the current ETag, the server returns
// 304 Not Modified with no body, saving bandwidth on reconnects.
func (s *Server) history(w http.ResponseWriter, r *http.Request) {
	writeJSONCached(w, r, s.projectedHistory())
}

// projectedHistory is the one reading of the message log a frontend is given:
// everything that serves a part of it goes through here, so a part can never
// carry more than the whole does.
func (s *Server) projectedHistory() []historyMessage {
	msgs := historyMessagesWith(s.ctl().History(), s.ctl().StoredAttachments)
	markHistoryDiffs(msgs, s.ctl().EmbeddedDiffDetectionEnabled())
	return msgs
}

func (s *Server) registerHistoryRoutes(parent *http.ServeMux) {
	mux := http.NewServeMux()
	foreground := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer backgroundwork.Shared.Foreground()()
		mux.ServeHTTP(w, r)
	})
	parent.Handle("GET /history", foreground)
	parent.Handle("GET /history/", foreground)
	mux.HandleFunc("GET /history", s.history)
	mux.HandleFunc("GET /history/page", s.historyPage)
	mux.HandleFunc("GET /history/tool/{id}", s.historyToolOutput)
}

// ToolOutputPage is one window onto the output of one tool call as /history
// carries it. Bytes is the whole length, Offset where Text starts, and
// Truncated says there is more after it.
type ToolOutputPage struct {
	CallID    string `json:"callId"`
	Text      string `json:"text"`
	Bytes     int    `json:"bytes"`
	Offset    int    `json:"offset"`
	Truncated bool   `json:"truncated"`
}

const (
	toolOutputDefault = 64 << 10
	toolOutputMax     = 256 << 10
)

// historyToolOutput serves a call's output a window at a time, for a page that
// kept only the ends of it. It reads the same projection /history does and the
// id is only a lookup key into it: nothing from the request names a file.
func (s *Server) historyToolOutput(w http.ResponseWriter, r *http.Request) {
	offset, limit, ok := toolOutputWindow(r)
	if !ok {
		refuse(w, http.StatusBadRequest, "tool.output_range", "offset and limit must be whole numbers, limit above zero", nil)
		return
	}
	id := r.PathValue("id")
	text, found := s.readHistoryToolOutput(id)
	if !found {
		refuse(w, http.StatusNotFound, "tool.output_unknown", "no tool call with this id in this session", nil)
		return
	}
	if offset > len(text) || (offset == len(text) && len(text) > 0) {
		refuse(w, http.StatusBadRequest, "tool.output_range", "offset is past the end of the output", nil)
		return
	}
	start := runeStartAtOrBefore(text, offset)
	end := runeStartAtOrBefore(text, min(len(text), start+limit))
	if end <= start && start < len(text) {
		end = min(len(text), start+utf8.UTFMax)
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, ToolOutputPage{CallID: id, Text: text[start:end], Bytes: len(text), Offset: start, Truncated: end < len(text)})
}

// toolOutputWindow reads offset and limit: whole non-negative numbers, the
// limit above zero and no larger than a response may carry.
func toolOutputWindow(r *http.Request) (offset, limit int, ok bool) {
	limit = toolOutputDefault
	q := r.URL.Query()
	if raw := q.Get("offset"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 0 {
			return 0, 0, false
		}
		offset = n
	}
	if raw := q.Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 {
			return 0, 0, false
		}
		limit = min(n, toolOutputMax)
	}
	return offset, limit, true
}

// runeStartAtOrBefore moves i back to the first byte of the character it falls in.
func runeStartAtOrBefore(s string, i int) int {
	for i > 0 && i < len(s) && !utf8.RuneStart(s[i]) {
		i--
	}
	return i
}

// markHistoryDiffs re-derives the whole-diff tag on a rebuilt transcript, since
// the live sink that sets it is gone once the session is recorded. Without this
// a shell `git diff` renders as a diff live and as flat text after reopening.
// The grammar itself rejects a truncated result, whose appended note leaves the
// text unparseable, so only the failure flag needs checking here.
func markHistoryDiffs(msgs []historyMessage, enabled bool) {
	if !enabled {
		return
	}
	for i := range msgs {
		m := &msgs[i]
		if m.Role == string(provider.RoleTool) && !m.ToolFailed && tool.IsShellTool(m.ToolName) {
			m.OutputDiff = diff.IsUnifiedDiff(m.Content)
		}
	}
}
