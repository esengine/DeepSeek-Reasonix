// Package serve exposes a control.Controller over HTTP: the typed event stream
// as Server-Sent Events, and the commands as small JSON POST endpoints. It is a
// second frontend alongside the chat TUI — proof that the controller is
// transport-agnostic, and the basis for a browser/desktop client. One server
// drives one session; multiple browser tabs share it.
package serve

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/boot"
	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/nilutil"
	"reasonix/internal/provider"
)

//go:embed index.html
var indexHTML []byte

// Server wires a controller to its HTTP surface. The Broadcaster must be the
// same sink the controller was constructed with, so events reach SSE clients.
type Server struct {
	mu        sync.RWMutex // guards ctrl, which switchModel swaps at runtime
	ctrl      *control.Controller
	bc        *Broadcaster
	titleProv provider.Provider // lightweight flash provider for session titles
	titles    *titleCache
}

// New builds a Server. bc must be the controller's event sink.
func New(ctrl *control.Controller, bc *Broadcaster) *Server {
	s := &Server{ctrl: ctrl, bc: bc, titles: newTitleCache(ctrl.SessionDir())}
	s.initTitleProvider()
	return s
}

// ctl returns the current controller. Handlers must read it through here, never
// the field directly, because switchModel replaces it under the write lock.
func (s *Server) ctl() *control.Controller {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.ctrl
}

// initTitleProvider builds a lightweight flash-model provider used solely to
// generate short session titles. Errors are silently swallowed — title
// generation is best-effort, and the server works fine without it.
func (s *Server) initTitleProvider() {
	cfg, err := config.Load()
	if err != nil {
		return
	}
	entry, ok := cfg.ResolveModel("deepseek-flash")
	if !ok {
		return
	}
	prov, err := provider.New(entry.Kind, provider.Config{
		Name:    entry.Name,
		BaseURL: entry.BaseURL,
		Model:   entry.Model,
		APIKey:  entry.APIKey(),
		Extra:   map[string]any{"effort": "off"},
	})
	if err != nil {
		return
	}
	s.titleProv = prov
}

// switchModel rebuilds the controller with a new model, carrying over the
// conversation history. This replicates the TUI/desktop model-switch path. The
// write lock is held across the whole rebuild so concurrent requests never read
// a half-swapped controller and two switches can't run at once.
func (s *Server) switchModel(ctx context.Context, ref string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	cur := s.ctrl
	if cur.Running() {
		return fmt.Errorf("cannot switch model while a turn is running")
	}
	prevPath := cur.SessionPath()
	if err := cur.Snapshot(); err != nil {
		slog.Warn("serve: snapshot before model switch", "err", err)
	}
	carried := cur.History()

	newCtrl, err := boot.Build(ctx, boot.Options{
		Model:  ref,
		Sink:   s.bc,
		Stderr: os.Stderr,
	})
	if err != nil {
		return fmt.Errorf("switch model: %w", err)
	}
	// Keep the carried conversation in its existing file so the switch doesn't
	// orphan a duplicate (#2807).
	newPath := agent.ContinueSessionPath(prevPath, newCtrl.SessionDir(), newCtrl.Label())
	if len(carried) > 0 {
		newCtrl.Resume(&agent.Session{Messages: carried}, newPath)
	} else if newPath != "" {
		newCtrl.SetSessionPath(newPath)
	}

	s.ctrl = newCtrl
	cur.Close()
	return nil
}

// switchEffort persists a new reasoning-effort level for the active provider and
// rebuilds via switchModel (which takes the write lock).
func (s *Server) switchEffort(ctx context.Context, level string) error {
	cur := s.ctl()
	if cur.Running() {
		return fmt.Errorf("cannot change effort while a turn is running")
	}
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	ref := cur.Label()
	entry, ok := cfg.ResolveModel(ref)
	if !ok {
		return fmt.Errorf("cannot resolve current provider %q", ref)
	}
	if !config.EffortCapabilityForEntry(entry).Supported {
		return fmt.Errorf("effort is not configurable for %s", entry.Name)
	}
	effort, err := config.NormalizeEffort(entry, level)
	if err != nil {
		return err
	}
	editPath := config.UserConfigPath()
	if editPath == "" {
		return fmt.Errorf("no config file found")
	}
	edit := config.LoadForEdit(editPath)
	if err := applyEffortEdit(edit, entry, effort); err != nil {
		return err
	}
	if err := edit.SaveTo(editPath); err != nil {
		return fmt.Errorf("save config: %w", err)
	}
	return s.switchModel(ctx, entry.Name+"/"+entry.Model)
}

// applyEffortEdit writes effort onto entry within edit, mirroring CLI/desktop
// SetEffort: upsert the provider when the user config has no block for it yet, and
// enable adaptive thinking for Anthropic so the effort knob actually engages.
func applyEffortEdit(edit *config.Config, entry *config.ProviderEntry, effort string) error {
	if _, ok := edit.Provider(entry.Name); !ok {
		if err := edit.UpsertProvider(*entry); err != nil {
			return err
		}
	}
	if entry.Kind == "anthropic" && effort != "" && entry.Thinking == "" {
		if err := edit.SetProviderThinking(entry.Name, "adaptive"); err != nil {
			return err
		}
	}
	return edit.SetProviderEffort(entry.Name, effort)
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
	mux.HandleFunc("POST /rewind", s.rewind)
	mux.HandleFunc("POST /fork", s.fork)
	mux.HandleFunc("POST /summarize", s.summarize)
	mux.HandleFunc("POST /tool-approval-mode", s.toolApprovalMode)
	mux.HandleFunc("POST /auto-approve-tools", s.autoApproveTools)
	mux.HandleFunc("POST /bypass", s.bypass)
	mux.HandleFunc("POST /answer", s.answer)
	mux.HandleFunc("POST /resume", s.resume)
	mux.HandleFunc("POST /forget", s.forget)
	mux.HandleFunc("GET /checkpoints", s.checkpoints)
	mux.HandleFunc("GET /branches", s.branches)
	mux.HandleFunc("GET /status", s.status)
	mux.HandleFunc("GET /sessions", s.sessions)
	mux.HandleFunc("GET /skills", s.skills)
	mux.HandleFunc("POST /delete-session", s.deleteSession)
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
	s.ctl().EnableInteractiveApproval()
	return http.ListenAndServe(addr, s.Handler())
}

// RunGraceful serves with graceful shutdown. It listens for SIGINT/SIGTERM on
// the provided context and drains active connections for up to 10 seconds
// before returning.
func (s *Server) RunGraceful(ctx context.Context, addr string) error {
	s.ctl().EnableInteractiveApproval()
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
	_, _ = config.MigrateLegacyIfNeeded()
	lang := "auto"
	if cfg, err := config.Load(); err == nil {
		if dl := cfg.DesktopLanguage(); dl != "" {
			lang = dl
		}
	}
	html := string(indexHTML)
	html = strings.ReplaceAll(html, "__LANG__", lang)
	_, _ = w.Write([]byte(html))
}

// sseKeepaliveInterval is how often the /events handler emits a `: ping`
// SSE comment. Most reverse proxies (nginx, ALB, Cloudflare) close idle
// upstream connections after 30–60 s; a long quiet turn (the agent
// thinking, the model generating a single long response) easily hits
// that window. The comment is one byte on the wire and is dropped by
// the EventSource client, so it's a no-op for the consumer while it
// keeps the TCP socket warm for the proxy.
const sseKeepaliveInterval = 15 * time.Second

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

	keepalive := time.NewTicker(sseKeepaliveInterval)
	defer keepalive.Stop()

	for {
		select {
		case data, ok := <-ch:
			if !ok {
				return
			}
			fmt.Fprintf(w, "data: %s\n\n", data)
			flusher.Flush()
		case <-keepalive.C:
			// SSE comment lines start with `:` and are ignored by the
			// client. Emit one every sseKeepaliveInterval so the
			// upstream socket stays warm; without this, a long quiet
			// turn (e.g. a model thinking) lets a proxy like nginx
			// or an ALB close the idle connection and the next
			// event arrives on a half-closed stream.
			fmt.Fprint(w, ": ping\n\n")
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
	trimmed := strings.TrimSpace(body.Input)
	// Intercept /model <ref> for runtime model switching (the controller's
	// Submit path only lists models — switching is frontend-specific).
	if strings.HasPrefix(trimmed, "/model ") {
		ref := strings.TrimSpace(strings.TrimPrefix(trimmed, "/model"))
		if ref != "" {
			if err := s.switchModel(r.Context(), ref); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
	}
	// Intercept /effort <level> for reasoning effort switching.
	if strings.HasPrefix(trimmed, "/effort ") {
		level := strings.TrimSpace(strings.TrimPrefix(trimmed, "/effort"))
		if level != "" {
			if err := s.switchEffort(r.Context(), level); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			w.WriteHeader(http.StatusNoContent)
			return
		}
	}
	s.ctl().Submit(body.Input)
	w.WriteHeader(http.StatusAccepted)
}

func (s *Server) cancel(w http.ResponseWriter, _ *http.Request) {
	s.ctl().Cancel()
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) approve(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID      string `json:"id"`
		Allow   bool   `json:"allow"`
		Session bool   `json:"session"`
		Persist bool   `json:"persist"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ID == "" {
		http.Error(w, "missing id", http.StatusBadRequest)
		return
	}
	s.ctl().Approve(body.ID, body.Allow, body.Session, body.Persist)
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
	s.ctl().SetPlanMode(body.On)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) compact(w http.ResponseWriter, r *http.Request) {
	if err := s.ctl().Compact(r.Context(), ""); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// Persist the compacted session to disk — ctrl.Compact() only mutates in-memory.
	if err := s.ctl().Snapshot(); err != nil {
		slog.Warn("serve: snapshot after compact", "err", err)
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) newSession(w http.ResponseWriter, _ *http.Request) {
	if err := s.ctl().NewSession(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type historyToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type historyMessage struct {
	Role       string            `json:"role"`
	Content    string            `json:"content"`
	Reasoning  string            `json:"reasoning,omitempty"`
	ToolCalls  []historyToolCall `json:"toolCalls,omitempty"`
	ToolCallID string            `json:"toolCallId,omitempty"`
	ToolName   string            `json:"toolName,omitempty"`
}

func historyMessages(msgs []provider.Message) []historyMessage {
	out := make([]historyMessage, 0, len(msgs))
	for _, m := range msgs {
		// Steer messages are surfaced as a notice, not a user message.
		if m.Role == provider.RoleUser {
			if steerText, isSteer := agent.SteerText(m.Content); isSteer {
				out = append(out, historyMessage{Role: "notice", Content: "↪ " + steerText})
				continue
			}
		}
		hm := historyMessage{Role: string(m.Role), Content: m.Content}
		if m.Role == provider.RoleAssistant {
			hm.Reasoning = m.ReasoningContent
			if len(m.ToolCalls) > 0 {
				hm.ToolCalls = make([]historyToolCall, len(m.ToolCalls))
				for i, tc := range m.ToolCalls {
					hm.ToolCalls[i] = historyToolCall{ID: tc.ID, Name: tc.Name, Arguments: tc.Arguments}
				}
			}
		}
		if m.Role == provider.RoleTool {
			hm.ToolCallID = m.ToolCallID
			hm.ToolName = m.Name
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
	writeJSONCached(w, r, historyMessages(s.ctl().History()))
}

// context returns the prompt-vs-window gauge numbers. Supports ETag caching
// so reconnecting clients avoid re-fetching unchanged context data.
func (s *Server) context(w http.ResponseWriter, r *http.Request) {
	used, window := s.ctl().ContextSnapshot()
	writeJSONCached(w, r, map[string]int{"used": used, "window": window})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Warn("serve: writeJSON encode failed", "err", err)
	}
}

// writeJSONCached encodes v as JSON, computes a weak ETag from the body, and
// returns 304 Not Modified if the client's If-None-Match matches. This avoids
// re-sending unchanged history/context payloads on every reconnect.
func writeJSONCached(w http.ResponseWriter, r *http.Request, v any) {
	body, err := json.Marshal(v)
	if err != nil {
		slog.Warn("serve: writeJSONCached marshal failed", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	etag := fmt.Sprintf(`"%x"`, sha256.Sum256(body))
	if match := r.Header.Get("If-None-Match"); match == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "private, max-age=0, must-revalidate")
	_, _ = w.Write(body)
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

// rewind rewinds the session to a checkpoint.
func (s *Server) rewind(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Turn  int    `json:"turn"`
		Scope string `json:"scope"` // "code", "conversation", "both"
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Turn < 0 {
		http.Error(w, "missing turn", http.StatusBadRequest)
		return
	}
	scope := control.RewindBoth
	switch body.Scope {
	case "code":
		scope = control.RewindCode
	case "conversation":
		scope = control.RewindConversation
	}
	if err := s.ctl().Rewind(body.Turn, scope); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// fork creates a new branch at a checkpoint.
func (s *Server) fork(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Turn int    `json:"turn"`
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Turn < 0 {
		http.Error(w, "missing turn", http.StatusBadRequest)
		return
	}
	path, err := s.ctl().ForkNamed(body.Turn, body.Name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]string{"path": path})
}

// summarize runs summarize-from or summarize-up-to on a turn.
func (s *Server) summarize(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Turn int    `json:"turn"`
		Mode string `json:"mode"` // "from" or "upto"
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Turn < 0 {
		http.Error(w, "missing turn", http.StatusBadRequest)
		return
	}
	var err error
	switch body.Mode {
	case "from":
		err = s.ctl().SummarizeFrom(r.Context(), body.Turn)
	case "upto":
		err = s.ctl().SummarizeUpTo(r.Context(), body.Turn)
	default:
		http.Error(w, "mode must be 'from' or 'upto'", http.StatusBadRequest)
		return
	}
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// autoApproveTools toggles YOLO/full-access tool auto-approval.
func (s *Server) autoApproveTools(w http.ResponseWriter, r *http.Request) {
	var body struct {
		On bool `json:"on"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad body", http.StatusBadRequest)
		return
	}
	s.ctl().SetAutoApproveTools(body.On)
	w.WriteHeader(http.StatusNoContent)
}

// toolApprovalMode selects ask, auto, or yolo approval behavior for interactive
// frontends. Plan remains a separate read-only gate.
func (s *Server) toolApprovalMode(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Mode string `json:"mode"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad body", http.StatusBadRequest)
		return
	}
	switch strings.ToLower(strings.TrimSpace(body.Mode)) {
	case control.ToolApprovalAsk, control.ToolApprovalAuto, control.ToolApprovalYolo:
		s.ctl().SetToolApprovalMode(body.Mode)
	default:
		http.Error(w, "mode must be ask, auto, or yolo", http.StatusBadRequest)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// bypass is the legacy HTTP endpoint for YOLO/full-access tool auto-approval.
func (s *Server) bypass(w http.ResponseWriter, r *http.Request) {
	s.autoApproveTools(w, r)
}

// answer responds to an ask_request.
func (s *Server) answer(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ID      string            `json:"id"`
		Answers []event.AskAnswer `json:"answers"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ID == "" {
		http.Error(w, "missing id", http.StatusBadRequest)
		return
	}
	s.ctl().AnswerQuestion(body.ID, body.Answers)
	w.WriteHeader(http.StatusNoContent)
}

// resume loads a previous session from a JSONL file.
func (s *Server) resume(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Path == "" {
		http.Error(w, "missing path", http.StatusBadRequest)
		return
	}
	// Snapshot the current session before switching away.
	if err := s.ctl().Snapshot(); err != nil {
		slog.Warn("serve: snapshot before resume", "err", err)
	}
	loaded, err := agent.LoadSession(body.Path)
	if err != nil {
		http.Error(w, "load session: "+err.Error(), http.StatusBadRequest)
		return
	}
	s.ctl().Resume(loaded, body.Path)
	w.WriteHeader(http.StatusNoContent)
}

// forget deletes a saved memory by name.
func (s *Server) forget(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		http.Error(w, "missing name", http.StatusBadRequest)
		return
	}
	if err := s.ctl().ForgetMemory(body.Name); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// checkpoints returns the session's checkpoint list for the rewind picker.
func (s *Server) checkpoints(w http.ResponseWriter, _ *http.Request) {
	type cp struct {
		Turn   int    `json:"turn"`
		Prompt string `json:"prompt"`
		Files  int    `json:"files"`
	}
	raw := s.ctl().Checkpoints()
	out := make([]cp, len(raw))
	for i, c := range raw {
		out[i] = cp{Turn: c.Turn, Prompt: c.Prompt, Files: len(c.Paths)}
	}
	writeJSON(w, out)
}

// branches returns the branch list and tree text.
func (s *Server) branches(w http.ResponseWriter, _ *http.Request) {
	branches, err := s.ctl().Branches()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	tree := s.ctl().BranchTreeText()
	writeJSON(w, map[string]any{"branches": branches, "tree": tree})
}

// status returns a combined status snapshot.
func (s *Server) status(w http.ResponseWriter, r *http.Request) {
	used, window := s.ctl().ContextSnapshot()
	hit, miss := s.ctl().SessionCache()
	sess := map[string]any{
		"label":            s.ctl().Label(),
		"running":          s.ctl().Running(),
		"plan":             s.ctl().PlanMode(),
		"autoApproveTools": s.ctl().AutoApproveTools(),
		"bypass":           s.ctl().AutoApproveTools(),
		"toolApprovalMode": s.ctl().ToolApprovalMode(),
		"goal":             s.ctl().Goal(),
		"goalStatus":       s.ctl().GoalStatus(),
		"cwd":              s.ctl().SessionDir(),
		"used":             used,
		"window":           window,
		"cacheHit":         hit,
		"cacheMiss":        miss,
	}
	if u := s.ctl().LastUsage(); u != nil {
		sess["lastUsage"] = u
	}
	if b, err := s.ctl().Balance(r.Context()); err == nil && b != nil {
		sess["balance"] = b
	}
	if j := s.ctl().Jobs(); len(j) > 0 {
		sess["jobs"] = j
	}
	writeJSON(w, sess)
}

const titlePrompt = `Generate a very short title (3-5 words max) for this conversation based on the user's first message. Reply with ONLY the title, no quotes, no punctuation at the end.`

// generateTitle calls a lightweight LLM to produce a short session title.
// Returns empty string on any error — callers should fall back to a preview.
func (s *Server) generateTitle(ctx context.Context, firstMsg string) string {
	if nilutil.IsNil(s.titleProv) || strings.TrimSpace(firstMsg) == "" {
		return ""
	}
	if r := []rune(firstMsg); len(r) > 300 {
		firstMsg = string(r[:300]) + "..."
	}
	ch, err := s.titleProv.Stream(ctx, provider.Request{
		Messages: []provider.Message{
			{Role: provider.RoleSystem, Content: titlePrompt},
			{Role: provider.RoleUser, Content: firstMsg},
		},
		Temperature: 0,
		MaxTokens:   20,
	})
	if err != nil {
		return ""
	}
	var text strings.Builder
	for chunk := range ch {
		switch chunk.Type {
		case provider.ChunkText:
			text.WriteString(chunk.Text)
		case provider.ChunkError:
			return ""
		}
	}
	title := strings.TrimSpace(text.String())
	if len(title) >= 2 && ((title[0] == '"' && title[len(title)-1] == '"') || (title[0] == '\'' && title[len(title)-1] == '\'')) {
		title = title[1 : len(title)-1]
	}
	return strings.TrimSpace(title)
}

// sessions lists saved session files from the session directory, enriched with
// LLM-generated titles and turn counts.
func (s *Server) sessions(w http.ResponseWriter, r *http.Request) {
	dir := s.ctl().SessionDir()
	if dir == "" {
		writeJSON(w, []any{})
		return
	}
	type sessionEntry struct {
		Name    string `json:"name"`
		Path    string `json:"path"`
		Title   string `json:"title,omitempty"`
		Turns   int    `json:"turns,omitempty"`
		Current bool   `json:"current,omitempty"`
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		writeJSON(w, []any{})
		return
	}
	current := filepath.Clean(s.ctl().SessionPath())
	var out []sessionEntry
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		name := strings.TrimSuffix(e.Name(), ".jsonl")
		entry := sessionEntry{Name: name, Path: path, Current: filepath.Clean(path) == current}
		if first, turns := previewSessionFile(path); turns > 0 {
			entry.Turns = turns
			entry.Title = s.sessionTitle(r.Context(), e.Name(), first, fileModNano(e))
		}
		out = append(out, entry)
	}
	// reverse so newest first
	for i, j := 0, len(out)-1; i < j; i, j = i+1, j-1 {
		out[i], out[j] = out[j], out[i]
	}
	if out == nil {
		out = []sessionEntry{}
	}
	writeJSON(w, out)
}

// deleteSession removes a saved session by the session name returned from /sessions.
func (s *Server) deleteSession(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		http.Error(w, "name required", http.StatusBadRequest)
		return
	}
	if name == "." || name == ".." || strings.ContainsAny(name, `/\`) {
		http.Error(w, "invalid session name", http.StatusBadRequest)
		return
	}
	dir := s.ctl().SessionDir()
	if dir == "" {
		http.Error(w, "sessions disabled", http.StatusBadRequest)
		return
	}
	target := filepath.Join(dir, name+".jsonl")
	abs, err := filepath.Abs(target)
	if err != nil {
		http.Error(w, "invalid session path", http.StatusBadRequest)
		return
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		http.Error(w, "invalid session dir", http.StatusBadRequest)
		return
	}
	rel, err := filepath.Rel(absDir, abs)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		http.Error(w, "path outside session dir", http.StatusForbidden)
		return
	}
	if filepath.Clean(abs) == filepath.Clean(s.ctl().SessionPath()) {
		http.Error(w, "cannot delete active session", http.StatusConflict)
		return
	}
	destroy := s.ctl().BeginDestroySession(abs)
	if err := os.Remove(abs); err != nil {
		go finishSessionDestroy(destroy)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if err := agent.DeleteSubagentsByParent(dir, agent.BranchID(abs)); err != nil {
		go finishSessionDestroy(destroy)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	go finishSessionDestroy(destroy)
	w.WriteHeader(http.StatusNoContent)
}

func finishSessionDestroy(destroy control.SessionDestroyHandle) {
	if destroy.Wait != nil {
		destroy.Wait()
	}
	if destroy.Finish != nil {
		destroy.Finish()
	}
}

// sessionTitle returns a title for a session: the cached flash-generated title
// when it matches the file's mtime, otherwise a freshly generated one (cached
// for next time), falling back to a truncated preview when generation is off.
func (s *Server) sessionTitle(ctx context.Context, name, first string, mod int64) string {
	if cached, ok := s.titles.get(name, mod); ok {
		return cached
	}
	if title := s.generateTitle(ctx, first); title != "" {
		s.titles.put(name, title, mod)
		return title
	}
	return previewTitle(first)
}

func previewTitle(first string) string {
	if r := []rune(first); len(r) > 50 {
		return string(r[:47]) + "..."
	}
	return first
}

func fileModNano(e os.DirEntry) int64 {
	info, err := e.Info()
	if err != nil {
		return 0
	}
	return info.ModTime().UnixNano()
}

// previewSessionFile reads the first user message and turn count from a JSONL session file.
func previewSessionFile(path string) (string, int) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0
	}
	defer f.Close()
	dec := json.NewDecoder(f)
	first := ""
	turns := 0
	for {
		var m struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		}
		if err := dec.Decode(&m); err != nil {
			break
		}
		if m.Role == "user" {
			turns++
			if first == "" {
				first = strings.TrimSpace(agent.HandoffTask(m.Content))
			}
		}
	}
	return first, turns
}

// skills lists discoverable skills.
func (s *Server) skills(w http.ResponseWriter, _ *http.Request) {
	type skillEntry struct {
		Name        string `json:"name"`
		Scope       string `json:"scope"`
		Subagent    bool   `json:"subagent"`
		Description string `json:"description"`
	}
	raw := s.ctl().Skills()
	out := make([]skillEntry, len(raw))
	for i, sk := range raw {
		out[i] = skillEntry{Name: sk.Name, Scope: string(sk.Scope), Subagent: sk.RunAs == "subagent", Description: sk.Description}
	}
	writeJSON(w, out)
}
