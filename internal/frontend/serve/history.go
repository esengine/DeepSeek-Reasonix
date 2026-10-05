package serve

import (
	"net/http"
	"reasonix/internal/state/sessionstore"

	"reasonix/internal/base/diff"
	"reasonix/internal/contract/eventwire"
	"reasonix/internal/contract/provider"
	"reasonix/internal/contract/tool"
)

type historyToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
	// What a stable proxy actually reached. Name stays the provider-visible
	// call, so a rebuild without these draws use_capability where the live
	// transcript drew the capability it resolved to.
	ResolvedName string `json:"resolvedName,omitempty"`
	CapabilityID string `json:"capabilityId,omitempty"`
}

type historyMessage struct {
	Role      string `json:"role"`
	Content   string `json:"content"`
	Reasoning string `json:"reasoning,omitempty"`
	// Images is how many attachments the turn carried, not the attachments
	// themselves: a turn that was only an image has no text to rebuild from,
	// and a reader that sees zero of both drops it as host chrome.
	Images int `json:"images,omitempty"`
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
	ModelRef   string            `json:"modelRef,omitempty"`
	ThoughtMs  int64             `json:"thoughtMs,omitempty"` // host-measured, so a reopened turn keeps it
	ToolCalls  []historyToolCall `json:"toolCalls,omitempty"`
	ToolCallID string            `json:"toolCallId,omitempty"`
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
	out := make([]historyMessage, 0, len(msgs))
	for i, m := range msgs {
		// A steer is the person speaking into a running turn, so it stays a
		// user message and carries what it was. The host's own mid-turn notice
		// rides the same prefix and is not theirs to have said.
		if m.Role == provider.RoleUser {
			if steerText, host, isSteer := sessionstore.SteerKind(m.Content); isSteer {
				out = append(out, historyMessage{
					Role: string(provider.RoleUser), Content: steerText, Steer: true,
					HostAuthored: host || m.HostAuthored, MsgIndex: i, Via: eventwire.ToWireVia(m.Via),
				})
				continue
			}
		}
		hm := historyMessage{Role: string(m.Role), Content: m.Content, MsgIndex: i, HostAuthored: m.HostAuthored}
		if m.Role == provider.RoleUser {
			// Content is what the model saw, and one @-reference expands into a
			// whole file. A reopened session has to show what was typed.
			hm.Content = sessionstore.UserMessageText(m)
			hm.Images = len(m.Images)
			hm.Via = eventwire.ToWireVia(m.Via)
		}
		if m.Role == provider.RoleAssistant {
			hm.ModelRef = m.ModelRef
			hm.Reasoning = m.ReasoningContent
			hm.ThoughtMs = m.ThoughtMs
			if len(m.ToolCalls) > 0 {
				hm.ToolCalls = make([]historyToolCall, len(m.ToolCalls))
				for i, tc := range m.ToolCalls {
					hm.ToolCalls[i] = historyToolCall{
						ID: tc.ID, Name: tc.Name, Arguments: tc.Arguments,
						ResolvedName: tc.ResolvedName, CapabilityID: tc.CapabilityID,
					}
				}
			}
		}
		if m.Role == provider.RoleTool {
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
	msgs := historyMessages(s.ctl().History())
	markHistoryDiffs(msgs, s.ctl().EmbeddedDiffDetectionEnabled())
	writeJSONCached(w, r, msgs)
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
