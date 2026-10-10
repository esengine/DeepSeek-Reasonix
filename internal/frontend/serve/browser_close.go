package serve

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"reasonix/internal/platform/browser"
	"reasonix/internal/session/control"
)

func (s *Server) browserClose(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Tab string `json:"tab"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&body); err != nil {
		badBody(w)
		return
	}
	if strings.TrimSpace(body.Tab) == "" {
		missingField(w, "tab")
		return
	}
	if err := s.ctl().BrowserClose(r.Context(), body.Tab); err != nil {
		params := map[string]any{"cause": string(browser.CodeOf(err))}
		refuse(w, browserCloseStatus(err), "browser.close_failed", err.Error(), params)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) registerBrowserRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /browser/tabs", s.browserTabs)
	mux.HandleFunc("POST /browser/open", s.browserOpen)
	mux.HandleFunc("POST /browser/close", s.browserClose)
}

func browserCloseStatus(err error) int {
	switch {
	case errors.Is(err, control.ErrBrowserUnavailable):
		return http.StatusNotFound
	case errors.Is(err, context.DeadlineExceeded):
		return http.StatusGatewayTimeout
	case errors.Is(err, context.Canceled):
		return http.StatusRequestTimeout
	}
	switch browser.CodeOf(err) {
	case browser.CodeEngineFailed:
		return http.StatusBadGateway
	case browser.CodeEngineMissing:
		return http.StatusServiceUnavailable
	case browser.CodeProfileBusy:
		return http.StatusConflict
	case browser.CodeNoTab:
		return http.StatusNotFound
	case browser.CodeTabClosed:
		return http.StatusGone
	case browser.CodeWaitTimeout, browser.CodeNavigationTimeout:
		return http.StatusGatewayTimeout
	case "":
		return http.StatusInternalServerError
	default:
		return http.StatusBadRequest
	}
}
