package serve

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/eventwire"
	"reasonix/internal/contract/provider"
	"reasonix/internal/session/control"
	"reasonix/internal/state/sessionstore"
)

type historyPageResponse struct {
	Messages      []historyMessage         `json:"messages"`
	ToolContext   []historyPageToolContext `json:"toolContext"`
	Snapshot      string                   `json:"snapshot"`
	Start         int                      `json:"start"`
	End           int                      `json:"end"`
	Total         int                      `json:"total"`
	HasMoreBefore bool                     `json:"hasMoreBefore"`
	NextCursor    string                   `json:"nextCursor,omitempty"`
	TurnStartedAt int64                    `json:"turnStartedAt,omitempty"`
}

type historyPageToolContext struct {
	MessageIndex int             `json:"messageIndex"`
	Ordinal      int             `json:"ordinal"`
	Call         historyToolCall `json:"call"`
}

type historyCursor struct {
	Identity string `json:"identity"`
	Anchor   int    `json:"anchor"`
	Before   int    `json:"before"`
}

type historyPageReader interface {
	ReadHistoryPage(sessionstore.HistoryPageRequest) (sessionstore.HistoryPageRead, error)
}

func (s *Server) readHistoryToolOutput(id string) (string, bool) {
	s.bindMu.Lock()
	defer s.bindMu.Unlock()
	if reader, ok := s.ctl().(interface{ ReadHistoryToolOutput(string) (string, bool) }); ok {
		return reader.ReadHistoryToolOutput(id)
	}
	var text string
	found := false
	for _, message := range s.projectedHistory() {
		if message.Role == string(provider.RoleTool) && message.ToolCallID == id {
			text, found = message.Content, true
		}
	}
	return text, found
}

func (s *Server) historyPage(w http.ResponseWriter, r *http.Request) {
	request, ok := parseHistoryPage(w, r)
	if !ok {
		return
	}
	s.bindMu.Lock()
	ctrl := s.ctl()
	reader, ok := ctrl.(historyPageReader)
	if !ok {
		s.bindMu.Unlock()
		refuse(w, http.StatusNotImplemented, "history.pagination_unsupported", "history paging is unavailable", nil)
		return
	}
	page, err := reader.ReadHistoryPage(request)
	if err != nil {
		s.bindMu.Unlock()
		if errors.Is(err, sessionstore.ErrHistorySnapshotChanged) {
			refuse(w, http.StatusConflict, "history.snapshot_changed", "history changed; reload the latest page", nil)
		} else {
			badValue(w, "cursor", "a valid history range")
		}
		return
	}
	attachments, stamp := pageAttachments(ctrl, page.Messages)
	diffs := ctrl.EmbeddedDiffDetectionEnabled()
	s.bindMu.Unlock()
	identity, err := json.Marshal(struct {
		Snapshot          string
		Start, End, Total int
		Diffs             bool
		Attachments       [][]event.Attachment
	}{page.Identity, page.Start, page.End, page.Total, diffs, stamp})
	if err != nil {
		refuse(w, http.StatusInternalServerError, "internal.failed", "history metadata could not be encoded", nil)
		return
	}
	etag := fmt.Sprintf(`"%x"`, sha256.Sum256(identity))
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "private, max-age=0, must-revalidate")
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	response := projectHistoryPage(page, func(text string) []event.Attachment { return attachments[text] }, diffs)
	writeJSON(w, response)
}

func parseHistoryPage(w http.ResponseWriter, r *http.Request) (sessionstore.HistoryPageRequest, bool) {
	request := sessionstore.HistoryPageRequest{Limit: 200}
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 || n > 500 {
			badValue(w, "limit", "an integer from 1 to 500")
			return request, false
		}
		request.Limit = n
	}
	if raw := r.URL.Query().Get("cursor"); raw != "" {
		if len(raw) > 1024 {
			badValue(w, "cursor", "a history cursor")
			return request, false
		}
		var cursor historyCursor
		data, err := base64.RawURLEncoding.DecodeString(raw)
		if err != nil || json.Unmarshal(data, &cursor) != nil || cursor.Identity == "" {
			badValue(w, "cursor", "a history cursor")
			return request, false
		}
		request.Identity, request.Anchor, request.Before = cursor.Identity, cursor.Anchor, cursor.Before
	}
	return request, true
}

func projectHistoryPage(page sessionstore.HistoryPageRead, attachments func(string) []event.Attachment, diffs bool) historyPageResponse {
	response := historyPageResponse{Snapshot: page.Identity, Start: page.Start, End: page.End, Total: page.Total,
		HasMoreBefore: page.Start > 0, TurnStartedAt: page.TurnStartedAt, ToolContext: []historyPageToolContext{}}
	calls := make(map[string]provider.ToolCall, len(page.ToolContext))
	for _, ref := range page.ToolContext {
		calls[ref.Call.ID] = ref.Call
		response.ToolContext = append(response.ToolContext, historyPageToolContext{MessageIndex: ref.MessageIndex, Ordinal: ref.Ordinal, Call: projectHistoryToolCall(ref.Call)})
	}
	response.Messages = historyMessageWindow(page.Messages, attachments, page.Start, page.WorkMs, calls)
	markHistoryDiffs(response.Messages, diffs)
	if response.HasMoreBefore {
		data, _ := json.Marshal(historyCursor{Identity: page.Identity, Anchor: page.Total, Before: page.Start})
		response.NextCursor = base64.RawURLEncoding.EncodeToString(data)
	}
	return response
}

func projectHistoryToolCall(tc provider.ToolCall) historyToolCall {
	return historyToolCall{ID: tc.ID, Name: tc.Name, Arguments: eventwire.SurfaceArgs(tc.Name, tc.ResolvedName, tc.ExecutionArguments()),
		ArgumentNormalization: eventwire.SurfaceArgumentNormalization(tc.Name, tc.ResolvedName, tc.ArgumentNormalization),
		ResolvedName:          tc.ResolvedName, CapabilityID: tc.CapabilityID}
}

func pageAttachments(ctrl control.SessionAPI, messages []provider.Message) (map[string][]event.Attachment, [][]event.Attachment) {
	byText := map[string][]event.Attachment{}
	stamp := [][]event.Attachment{}
	for _, message := range messages {
		if message.Role != provider.RoleUser || message.HostAuthored {
			continue
		}
		text := sessionstore.UserMessageText(message)
		if steer, host, ok := sessionstore.SteerKind(message.Content); ok {
			if host {
				continue
			}
			text = steer
			if message.RawContent != "" {
				text = message.RawContent
			}
		}
		files := ctrl.StoredAttachments(text)
		byText[text] = files
		stamp = append(stamp, files)
	}
	return byText, stamp
}
