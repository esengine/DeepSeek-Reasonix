package serve

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"sync"

	"reasonix/internal/base/secrets"
	"reasonix/internal/contract/event"
	"reasonix/internal/session/control"
)

const (
	codeSideUnavailable = "side.unavailable"
	codeSideUnknown     = "side.unknown"
	codeSideBusy        = "side.busy"
	codeSideLimit       = "side.limit"
	codeSideEmpty       = "side.empty"
	codeSideFailed      = "side.failed"
)

// SideProgress is private to one ask request, never part of the main event log.
// Reasoning contents and tool arguments/results are deliberately absent.
type SideProgress struct {
	Kind string `json:"kind"`
	Text string `json:"text,omitempty"`
	Tool string `json:"tool,omitempty"`
}

func (s *Server) registerSideRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /side", s.openSide)
	mux.HandleFunc("POST /side/{id}/ask", s.askSide)
	mux.HandleFunc("DELETE /side/{id}", s.closeSide)
}

// openSide starts an empty side conversation. It is held by this runtime and
// gone with it; nothing about it is written anywhere.
func (s *Server) openSide(w http.ResponseWriter, _ *http.Request) {
	id, err := s.ctl().OpenSide()
	if err != nil {
		refuseSide(w, err)
		return
	}
	writeJSON(w, map[string]any{"id": id})
}

// askSide retains the JSON answer for existing clients; streaming clients opt
// into request-scoped progress through Accept, not a separate route.
func (s *Server) askSide(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	var stream *sideStream
	if acceptsSideStream(r) {
		if _, ok := w.(http.Flusher); !ok {
			refuse(w, http.StatusInternalServerError, "stream.unsupported", "this transport cannot stream", nil)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("X-Accel-Buffering", "no")
		stream = &sideStream{w: w, cancel: cancel}
	}
	var req struct {
		Question string   `json:"question"`
		Quotes   []string `json:"quotes"`
	}
	err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req)
	if stream != nil {
		stream.write("progress", SideProgress{Kind: "waiting"}, false)
		if ctx.Err() != nil {
			return
		}
	}
	if err != nil {
		if stream != nil {
			stream.fail(http.StatusBadRequest, Reason{Code: codeBadBody, Message: "the request body could not be read"})
		} else {
			badBody(w)
		}
		return
	}
	var progress event.Sink
	if stream != nil {
		progress = event.FuncSink(stream.emit)
	}
	answer, err := s.ctl().AskSide(ctx, r.PathValue("id"), req.Question, req.Quotes, progress)
	if err != nil {
		if stream != nil {
			status, reason := sideReason(err)
			stream.fail(status, reason)
		} else {
			refuseSide(w, err)
		}
		return
	}
	if stream != nil {
		stream.write("done", struct {
			Answer string `json:"answer"`
		}{answer}, true)
		return
	}
	writeJSON(w, map[string]any{"answer": answer})
}

func (s *Server) closeSide(w http.ResponseWriter, r *http.Request) {
	s.ctl().CloseSide(r.PathValue("id"))
	w.WriteHeader(http.StatusNoContent)
}

func refuseSide(w http.ResponseWriter, err error) {
	status, reason := sideReason(err)
	refuse(w, status, reason.Code, reason.Message, reason.Params)
}

// sideReason is shared by JSON and SSE so transport cannot change a refusal's
// identity or expose unsanitized provider credentials in its detail.
func sideReason(err error) (int, Reason) {
	switch {
	case errors.Is(err, control.ErrSideUnavailable):
		return http.StatusNotFound, Reason{Code: codeSideUnavailable, Message: "side questions are not available in this session"}
	case errors.Is(err, control.ErrSideUnknown):
		return http.StatusNotFound, Reason{Code: codeSideUnknown, Message: "that side question is closed"}
	case errors.Is(err, control.ErrSideBusy):
		return http.StatusConflict, Reason{Code: codeSideBusy, Message: "this side question is still being answered"}
	case errors.Is(err, control.ErrSideLimit):
		return http.StatusConflict, Reason{Code: codeSideLimit, Message: "too many side questions are open"}
	case errors.Is(err, control.ErrSideEmpty):
		return http.StatusBadRequest, Reason{Code: codeSideEmpty, Message: "a side question needs some words"}
	}
	if reason, ok := memoryReadReason(err); ok {
		return http.StatusInternalServerError, reason
	}
	if reason, ok := unparsedConfigReason(err); ok {
		return http.StatusConflict, reason
	}
	var c *coded
	if errors.As(err, &c) {
		return c.status, c.reason
	}
	detail := secrets.RedactCredentials(err.Error())
	return http.StatusBadGateway, Reason{Code: codeSideFailed, Message: detail, Params: map[string]any{"detail": detail}}
}

func acceptsSideStream(r *http.Request) bool {
	for _, value := range r.Header.Values("Accept") {
		for part := range strings.SplitSeq(value, ",") {
			kind, params, err := mime.ParseMediaType(strings.TrimSpace(part))
			if err != nil || kind != "text/event-stream" {
				continue
			}
			if q, ok := params["q"]; ok {
				weight, err := strconv.ParseFloat(q, 64)
				if err != nil || weight <= 0 || weight > 1 {
					continue
				}
			}
			return true
		}
	}
	return false
}

// Writes apply backpressure rather than queuing deltas. The lock also excludes
// late concurrent tool callbacks after the terminal frame. A failed write or
// flush cancels the run, just as a disconnected request does.
type sideStream struct {
	mu       sync.Mutex
	w        http.ResponseWriter
	cancel   context.CancelFunc
	closed   bool
	progress SideProgress
}

func (s *sideStream) write(name string, value any, terminal bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	if progress, ok := value.(SideProgress); ok {
		if progress.Kind != "text" && progress == s.progress {
			return
		}
		s.progress = SideProgress{Kind: progress.Kind, Tool: progress.Tool}
	}
	data, err := json.Marshal(value)
	if err == nil {
		_, err = fmt.Fprintf(s.w, "event: %s\ndata: %s\n\n", name, data)
	}
	if err == nil {
		err = http.NewResponseController(s.w).Flush()
	}
	if err != nil {
		s.cancel()
	}
	s.closed = terminal || err != nil
}

func (s *sideStream) fail(status int, reason Reason) {
	s.write("error", struct {
		Reason
		Status int `json:"status"`
	}{reason, status}, true)
}

func (s *sideStream) emit(e event.Event) {
	var progress SideProgress
	switch e.Kind {
	case event.Text:
		progress = SideProgress{Kind: "text", Text: e.Text}
	case event.Reasoning, event.ToolResult:
		progress = SideProgress{Kind: "thinking"}
	case event.ToolDispatch:
		progress = SideProgress{Kind: "tool", Tool: e.Tool.Name}
	default:
		return
	}
	s.write("progress", progress, false)
}
