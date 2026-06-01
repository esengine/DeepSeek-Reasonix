// Package serve exposes a control.Controller over HTTP: the typed event stream
// as Server-Sent Events, and the commands as small JSON POST endpoints. It is a
// second frontend alongside the chat TUI — proof that the controller is
// transport-agnostic, and the basis for a browser/desktop client. One server
// drives one session; multiple browser tabs share it.
package serve

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"reasonix/internal/control"
)

//go:embed index.html
var indexHTML []byte

// Server wires a controller to its HTTP surface. The Broadcaster must be the
// same sink the controller was constructed with, so events reach SSE clients.
type Server struct {
	ctrl *control.Controller
	bc   *Broadcaster
}

// New builds a Server. bc must be the controller's event sink.
func New(ctrl *control.Controller, bc *Broadcaster) *Server {
	return &Server{ctrl: ctrl, bc: bc}
}

// Handler returns the HTTP routes: GET / (a minimal browser client), GET /events
// (SSE), GET /history, GET /context, and POST command endpoints.
// CORS is NOT applied by default — same-origin policy protects the unauthenticated
// agent endpoints. Call HandlerWithCORS to opt in for local development.
func (s *Server) Handler() http.Handler {
	return s.handler()
}

// HandlerWithCORS returns the same routes as Handler but adds permissive CORS
// headers so a dev frontend on a different origin (e.g. Vite on :5173) can
// reach the server. Do NOT use in production — the server has no auth.
func (s *Server) HandlerWithCORS(origin string) http.Handler {
	return corsMiddleware(s.handler(), origin)
}

func (s *Server) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", s.index)
	mux.HandleFunc("GET /events", s.events)
	mux.HandleFunc("GET /history", s.history)
	mux.HandleFunc("GET /context", s.context)
	mux.HandleFunc("POST /submit", s.submit)
	mux.HandleFunc("POST /cancel", s.cancel)
	mux.HandleFunc("POST /approve", s.approve)
	mux.HandleFunc("POST /plan", s.plan)
	mux.HandleFunc("POST /compact", s.compact)
	mux.HandleFunc("POST /new", s.newSession)
	return logMiddleware(csrfGuard(mux))
}

// csrfGuard rejects state-changing requests that don't carry a JSON content type.
// The command endpoints have no auth and bind to localhost, so a page the user
// visits could otherwise drive them with a simple cross-origin POST (text/plain,
// no preflight) — submitting prompts or auto-approving tool calls. Requiring
// application/json forces a CORS preflight the unauthenticated server never
// answers, blocking cross-site requests; the same-origin frontend (which always
// sends JSON) is unaffected.
func csrfGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			ct := r.Header.Get("Content-Type")
			if i := strings.IndexByte(ct, ';'); i >= 0 {
				ct = ct[:i]
			}
			if strings.TrimSpace(ct) != "application/json" {
				http.Error(w, "Content-Type must be application/json", http.StatusUnsupportedMediaType)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// Run serves until the process is killed. Interactive approval is enabled so
// "ask" decisions surface as approval_request events answered via POST /approve.
func (s *Server) Run(addr string) error {
	s.ctrl.EnableInteractiveApproval()
	return http.ListenAndServe(addr, s.Handler())
}

// RunGraceful serves with graceful shutdown. It listens for SIGINT/SIGTERM on
// the provided context and drains active connections for up to 10 seconds
// before returning.
func (s *Server) RunGraceful(ctx context.Context, addr string) error {
	s.ctrl.EnableInteractiveApproval()
	srv := &http.Server{
		Addr:              addr,
		Handler:           s.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}
	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.ListenAndServe()
	}()
	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		slog.Info("serve: shutting down gracefully")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			slog.Warn("serve: graceful shutdown failed", "err", err)
		}
		return <-errCh
	}
}

func (s *Server) index(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(indexHTML)
}

// events streams the controller's event flow as SSE until the client
// disconnects. Each event is one `data:` frame of the JSON wire form.
func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch, unsubscribe := s.bc.Subscribe()
	defer unsubscribe()

	fmt.Fprint(w, ": connected\n\n") // open the stream immediately
	flusher.Flush()

	for {
		select {
		case data, ok := <-ch:
			if !ok {
				return
			}
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

// submit runs raw user input as a turn (slash commands and @-references
// resolved by the controller). Returns 202 — output arrives on the event stream.
func (s *Server) submit(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Input string `json:"input"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Input == "" {
		http.Error(w, "missing input", http.StatusBadRequest)
		return
	}
	s.ctrl.Submit(body.Input)
	w.WriteHeader(http.StatusAccepted)
}

func (s *Server) cancel(w http.ResponseWriter, _ *http.Request) {
	s.ctrl.Cancel()
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) approve(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID      string `json:"id"`
		Allow   bool   `json:"allow"`
		Session bool   `json:"session"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ID == "" {
		http.Error(w, "missing id", http.StatusBadRequest)
		return
	}
	s.ctrl.Approve(body.ID, body.Allow, body.Session)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) plan(w http.ResponseWriter, r *http.Request) {
	var body struct {
		On bool `json:"on"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad body", http.StatusBadRequest)
		return
	}
	s.ctrl.SetPlanMode(body.On)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) compact(w http.ResponseWriter, r *http.Request) {
	if err := s.ctrl.Compact(r.Context(), ""); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) newSession(w http.ResponseWriter, _ *http.Request) {
	if err := s.ctrl.NewSession(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// history returns the session's message log as {role, content} pairs so a
// reconnecting client can repopulate its transcript.
func (s *Server) history(w http.ResponseWriter, _ *http.Request) {
	type msg struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	var out []msg
	for _, m := range s.ctrl.History() {
		out = append(out, msg{Role: string(m.Role), Content: m.Content})
	}
	writeJSON(w, out)
}

// context returns the prompt-vs-window gauge numbers.
func (s *Server) context(w http.ResponseWriter, _ *http.Request) {
	used, window := s.ctrl.ContextSnapshot()
	writeJSON(w, map[string]int{"used": used, "window": window})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Warn("serve: writeJSON encode failed", "err", err)
	}
}

// corsMiddleware adds CORS headers for a specific allowed origin. Only use for
// local development — the server has no auth, so broad CORS would let any site
// drive the agent. origin is the exact origin to allow (e.g.
// "http://localhost:5173"); empty origin skips CORS entirely.
func corsMiddleware(next http.Handler, origin string) http.Handler {
	if origin == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", origin)
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// logMiddleware logs each request's method, path, and status.
func logMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &responseWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rw, r)
		slog.Info("serve: request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rw.status,
			"duration", time.Since(start).String(),
		)
	})
}

// responseWriter captures the status code for logging.
type responseWriter struct {
	http.ResponseWriter
	status int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.status = code
	rw.ResponseWriter.WriteHeader(code)
}

// Flush delegates to the underlying ResponseWriter if it supports flushing
// (required for SSE /events). Without this the type assertion in the events
// handler fails and the stream endpoint returns 500.
func (rw *responseWriter) Flush() {
	if f, ok := rw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
