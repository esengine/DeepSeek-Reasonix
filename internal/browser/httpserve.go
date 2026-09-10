package browser

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
)

// httpHandler serves the contract over any Executor: constant-time bearer
// check, bounded JSON bodies, sentinels as 409 bodies, everything else 500.
type httpHandler struct {
	exec  Executor
	token string
	mux   *http.ServeMux
}

// NewHTTPHandler exposes exec at /v1/browser/<method> behind a bearer token.
// GET /v1/browser/health answers 204 while exec is available, 503 otherwise.
func NewHTTPHandler(exec Executor, token string) http.Handler {
	h := &httpHandler{exec: exec, token: strings.TrimSpace(token), mux: http.NewServeMux()}
	h.mux.HandleFunc("GET "+httpHealthRoute, h.health)
	h.mux.HandleFunc("POST "+httpRoutePrefix+"tabs", h.tabs)
	h.mux.HandleFunc("POST "+httpRoutePrefix+"open", h.open)
	h.mux.HandleFunc("POST "+httpRoutePrefix+"navigate", h.navigate)
	h.mux.HandleFunc("POST "+httpRoutePrefix+"snapshot", h.snapshot)
	h.mux.HandleFunc("POST "+httpRoutePrefix+"screenshot", h.screenshot)
	h.mux.HandleFunc("POST "+httpRoutePrefix+"act", h.act)
	h.mux.HandleFunc("POST "+httpRoutePrefix+"downloads", h.downloads)
	h.mux.HandleFunc("POST "+httpRoutePrefix+"close", h.close)
	return h
}

func (h *httpHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if !h.authorized(r) {
		w.Header().Set("WWW-Authenticate", `Bearer realm="reasonix-browser"`)
		writeWireError(w, http.StatusUnauthorized, "unauthorized", "invalid browser broker token")
		return
	}
	h.mux.ServeHTTP(w, r)
}

func (h *httpHandler) authorized(r *http.Request) bool {
	prefix, value, ok := strings.Cut(strings.TrimSpace(r.Header.Get("Authorization")), " ")
	if !ok || !strings.EqualFold(prefix, "Bearer") || h.token == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(strings.TrimSpace(value)), []byte(h.token)) == 1
}

func (h *httpHandler) health(w http.ResponseWriter, r *http.Request) {
	if a, ok := h.exec.(Availability); ok && !a.Available(sessionContext(r)) {
		writeWireError(w, http.StatusServiceUnavailable, "unavailable", "no browser is available for this session")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func sessionContext(r *http.Request) context.Context {
	return WithSession(r.Context(), strings.TrimSpace(r.Header.Get(SessionHeader)))
}

// decode reads a bounded JSON body into in; a false return means the reply
// was already written.
func decodeBody(w http.ResponseWriter, r *http.Request, in any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, httpMaxRequestBytes)
	if err := json.NewDecoder(r.Body).Decode(in); err != nil {
		writeWireError(w, http.StatusBadRequest, "bad_request", "invalid JSON body: "+err.Error())
		return false
	}
	return true
}

func writeWireError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(wireError{Error: code, Message: message})
}

// writeResult maps err onto the wire: sentinels become 409 with their code,
// anything else is a 500 the client reports as a plain error.
func writeResult(w http.ResponseWriter, v any, err error) {
	if err != nil {
		for _, code := range []string{wireStaleReference, wireTakenOver, wireNoGrant, wireUnknownOutcome} {
			if errors.Is(err, wireErrorCodes[code]) {
				writeWireError(w, http.StatusConflict, code, err.Error())
				return
			}
		}
		writeWireError(w, http.StatusInternalServerError, "failed", err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func (h *httpHandler) tabs(w http.ResponseWriter, r *http.Request) {
	var in struct{}
	if !decodeBody(w, r, &in) {
		return
	}
	tabs, err := h.exec.Tabs(sessionContext(r))
	out := wireTabs{Tabs: make([]wireTab, 0, len(tabs))}
	for _, t := range tabs {
		out.Tabs = append(out.Tabs, toWireTab(t))
	}
	writeResult(w, out, err)
}

func (h *httpHandler) open(w http.ResponseWriter, r *http.Request) {
	var in wireOpenRequest
	if !decodeBody(w, r, &in) {
		return
	}
	tab, err := h.exec.Open(sessionContext(r), OpenRequest(in))
	writeResult(w, toWireTab(tab), err)
}

func (h *httpHandler) navigate(w http.ResponseWriter, r *http.Request) {
	var in wireNavigateRequest
	if !decodeBody(w, r, &in) {
		return
	}
	tab, err := h.exec.Navigate(sessionContext(r), NavigateRequest(in))
	writeResult(w, toWireTab(tab), err)
}

func (h *httpHandler) snapshot(w http.ResponseWriter, r *http.Request) {
	var in wireSnapshotRequest
	if !decodeBody(w, r, &in) {
		return
	}
	snap, err := h.exec.Snapshot(sessionContext(r), SnapshotRequest(in))
	writeResult(w, wireSnapshot(snap), err)
}

func (h *httpHandler) screenshot(w http.ResponseWriter, r *http.Request) {
	var in wireScreenshotRequest
	if !decodeBody(w, r, &in) {
		return
	}
	shot, err := h.exec.Screenshot(sessionContext(r), ScreenshotRequest(in))
	writeResult(w, wireScreenshot(shot), err)
}

func (h *httpHandler) act(w http.ResponseWriter, r *http.Request) {
	var in wireActRequest
	if !decodeBody(w, r, &in) {
		return
	}
	res, err := h.exec.Act(sessionContext(r), in.request())
	writeResult(w, wireActResult(res), err)
}

func (h *httpHandler) downloads(w http.ResponseWriter, r *http.Request) {
	var in wireDownloadsRequest
	if !decodeBody(w, r, &in) {
		return
	}
	downloads, err := h.exec.Downloads(sessionContext(r), in.request())
	out := wireDownloads{Downloads: make([]wireDownload, 0, len(downloads))}
	for _, d := range downloads {
		out.Downloads = append(out.Downloads, wireDownload(d))
	}
	writeResult(w, out, err)
}

func (h *httpHandler) close(w http.ResponseWriter, r *http.Request) {
	var in wireCloseRequest
	if !decodeBody(w, r, &in) {
		return
	}
	writeResult(w, struct{}{}, h.exec.Close(sessionContext(r), CloseRequest(in)))
}
