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
	"errors"
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
	"reasonix/internal/jobs"
	"reasonix/internal/nilutil"
	"reasonix/internal/provider"
	"reasonix/internal/store"
)

//go:embed index.html
var indexHTML []byte

//go:embed logo-wordmark.svg
var logoWordmarkSVG []byte

// Server wires a controller to its HTTP surface. The Broadcaster must be the
// same sink the controller was constructed with, so events reach SSE clients.
type Server struct {
	mu sync.RWMutex // guards ctrl, which switchModel swaps at runtime
	// bindMu serializes every entry point that changes the active session
	// path — /resume, /new, /fork, and switchModel. net/http runs handlers
	// concurrently and serve serves multiple browser tabs, so without this
	// two interleaved rebinds can leave the controller writing one session
	// while the lease keeper guards another (the exact split this feature
	// exists to prevent). It also keeps switchModel's Snapshot/Build/Close
	// off s.mu, as the narrower switchMu did before it was widened.
	bindMu sync.Mutex
	ctrl   control.SessionAPI
	bc     *Broadcaster
	// buildController builds the replacement controller during a model switch.
	// Nil in production (switchModel falls back to boot.Build); tests inject a
	// fake so switchModel can be exercised without real provider IO.
	buildController func(ctx context.Context, ref string) (*control.Controller, error)
	titleProv       provider.Provider // lightweight flash provider for session titles
	titlePrice      *provider.Pricing
	titles          *titleCache
	auth            *authGate // nil when auth is disabled
	// leases guards the active session file against other runtimes (a desktop
	// window, another CLI). Wired by the serve CLI command with the keeper that
	// already holds the startup session's lease; nil (tests, embedded use)
	// disables lease gating.
	leases *control.SessionLeaseKeeper
}

// New builds a Server. bc must be the controller's event sink.
// serveCfg controls authentication (none, token, or password).
func New(ctrl control.SessionAPI, bc *Broadcaster, serveCfg config.ServeConfig) *Server {
	s := &Server{
		ctrl:   ctrl,
		bc:     bc,
		titles: newTitleCache(ctrl.SessionDir()),
		auth:   newAuthGate(serveCfg),
	}
	s.initTitleProvider()
	return s
}

// ctl returns the current controller. Handlers must read it through here, never
// the field directly, because switchModel replaces it under the write lock.
func (s *Server) ctl() control.SessionAPI {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.ctrl
}

// SetSessionLeases hands the server the session-lease keeper that guards its
// active session file. The write-binding endpoints (/resume, /new, /fork and
// model switches that rotate the path) then move the lease along with the
// active session and refuse to bind a session held by another runtime.
// Call it before serving; a nil keeper leaves lease gating off.
func (s *Server) SetSessionLeases(k *control.SessionLeaseKeeper) {
	s.leases = k
}

// rebindSessionLease moves the server's session lease to path. A nil keeper
// gates nothing (tests, embedded use).
func (s *Server) rebindSessionLease(path string) error {
	if s.leases == nil {
		return nil
	}
	return s.leases.Rebind(path)
}

// resumeBindHookForTest, when set, runs inside /resume's critical sequence
// between the lease rebind and the controller Resume. Tests use it to force
// the interleaving bindMu exists to prevent; production never sets it.
var resumeBindHookForTest func()

// sessionInUseError renders a lease refusal for HTTP clients using the shared
// CLI wording, without the session file path.
func sessionInUseError(err error) string {
	return control.SessionInUseMessage(err) + "; " + control.SessionLeaseCloseHint
}

// AuthToken returns the pre-shared token when in token mode, or "" otherwise.
func (s *Server) AuthToken() string {
	if s.auth == nil {
		return ""
	}
	return s.auth.Token()
}

// AuthMode returns the authentication mode: "none", "token", or "password".
func (s *Server) AuthMode() string {
	if s.auth == nil {
		return "none"
	}
	return s.auth.Mode()
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
	s.titlePrice = entry.Price
}

// switchModel rebuilds the controller with a new model, carrying over the
// conversation history. This replicates the TUI/desktop model-switch path.
//
// The heavy steps — Snapshot (may touch disk), Build (provider init IO), and the
// old controller's Close (jobs.CloseWithGrace up to 15s + SessionEnd hook) — all
// run OFF s.mu. Holding the write lock across them would wedge every HTTP handler
// on s.ctl()'s RLock for the duration, stalling the whole serve frontend
// (mirrors the acp rebuildSession fix and PR #5920). bindMu serializes the
// switch against every other session-path-changing entry point (/resume,
// /new, /fork), preserving the old "second switch waits" semantics without
// pinning s.mu.
func (s *Server) switchModel(ctx context.Context, ref string) error {
	s.bindMu.Lock()
	defer s.bindMu.Unlock()

	// Snapshot the current controller under a short read of s.mu only.
	cur := s.ctl()
	if cur.Running() {
		return fmt.Errorf("cannot switch model while a turn is running")
	}

	// Off-lock: snapshot, carry history, and build the replacement. None of these
	// touch s.mu, so concurrent handlers keep reading the live controller.
	if err := cur.Snapshot(); err != nil {
		slog.Warn("serve: snapshot before model switch", "err", err)
	}
	// Capture the continue path and history only after Snapshot: a snapshot
	// conflict can retarget cur to a recovery branch (or adopt the newer disk
	// transcript), and a pre-snapshot capture would bind the rebuilt controller
	// back to the original file, re-conflicting on every later save.
	prevPath := cur.SessionPath()
	carried := cur.History()

	newCtrl, err := s.build(ctx, ref)
	if err != nil {
		return fmt.Errorf("switch model: %w", err)
	}
	// Keep the carried conversation in its existing file so the switch doesn't
	// orphan a duplicate (#2807).
	newPath := agent.ContinueSessionPath(prevPath, newCtrl.SessionDir(), newCtrl.Label())
	newCtrl.AdoptHistory(carried, newPath)

	// Publish the swap under a short write lock. bindMu already serializes
	// switches — today the only writer of s.ctrl — so the identity re-check is
	// defensive: it keeps a future controller-swapping path (or a test doing so)
	// from being silently clobbered after the off-lock build. On a mismatch,
	// discard the fresh controller off-lock instead of leaking it.
	s.mu.Lock()
	if s.ctrl != cur {
		s.mu.Unlock()
		newCtrl.Close()
		return fmt.Errorf("switch model: session changed during switch")
	}
	s.ctrl = newCtrl
	s.mu.Unlock()

	// The lease follows the active session file. Rebind is a no-op for the
	// common carried case (newPath == held path); it moves when a previously
	// file-less session got a fresh path here, or when the pre-switch snapshot
	// recovered onto a recovery branch. Both targets are fresh files created
	// by this process, so failure is theoretical.
	if err := s.rebindSessionLease(newPath); err != nil {
		slog.Warn("serve: session lease after model switch", "err", err)
	}

	// Off-lock: tear down the old controller. Close can block up to 15s.
	cur.Close()
	return nil
}

// build returns the replacement controller for a model switch, using the
// injected builder in tests and boot.Build in production.
func (s *Server) build(ctx context.Context, ref string) (*control.Controller, error) {
	if s.buildController != nil {
		return s.buildController(ctx, ref)
	}
	return boot.Build(ctx, boot.Options{
		Model:  ref,
		Sink:   s.bc,
		Stderr: os.Stderr,
	})
}

// switchEffort persists a new reasoning-effort level for the active provider and
// rebuilds via switchModel (which serializes on bindMu).
func (s *Server) switchEffort(ctx context.Context, level string) error {
	cur := s.ctl()
	if cur.Running() {
		return fmt.Errorf("cannot change effort while a turn is running")
	}
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	ref := currentModelRef(cur)
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
	// Lock only the load-modify-save cycle; switchModel below rebuilds the
	// controller and must not hold the config edit lock.
	if err := func() error {
		unlock := config.LockUserConfigEdits()
		defer unlock()
		edit := config.LoadForEdit(editPath)
		if err := applyEffortEdit(edit, entry, effort); err != nil {
			return err
		}
		if err := edit.SaveTo(editPath); err != nil {
			return fmt.Errorf("save config: %w", err)
		}
		return nil
	}(); err != nil {
		return err
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
	mux.HandleFunc("GET /assets/logo-wordmark.svg", s.logoWordmark)
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
	mux.HandleFunc("POST /goal", s.goal)
	mux.HandleFunc("POST /answer", s.answer)
	mux.HandleFunc("POST /resume", s.resume)
	mux.HandleFunc("POST /forget", s.forget)
	mux.HandleFunc("GET /checkpoints", s.checkpoints)
	mux.HandleFunc("GET /branches", s.branches)
	mux.HandleFunc("GET /models", s.models)
	mux.HandleFunc("GET /status", s.status)
	mux.HandleFunc("GET /sessions", s.sessions)
	mux.HandleFunc("GET /skills", s.skills)
	mux.HandleFunc("GET /todos", s.todos)
	mux.HandleFunc("POST /delete-session", s.deleteSession)
	return logMiddleware(s.auth.middleware(csrfGuard(mux)))
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
			if !strings.EqualFold(strings.TrimSpace(ct), "application/json") {
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

func (s *Server) logoWordmark(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "image/svg+xml; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	_, _ = w.Write(logoWordmarkSVG)
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
	if strings.HasPrefix(trimmed, "!") {
		http.Error(w, "shell commands are unavailable over HTTP", http.StatusForbidden)
		return
	}
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
	s.ctl().SubmitHTTP(body.Input)
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
	// Session-path-changing entry point: serialize with /resume, /fork, and
	// switchModel so the controller and the lease keeper move together.
	s.bindMu.Lock()
	defer s.bindMu.Unlock()
	if err := s.ctl().NewSession(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// Fresh path — the lease follows it; failure is theoretical but not silent.
	if err := s.rebindSessionLease(s.ctl().SessionPath()); err != nil {
		http.Error(w, sessionInUseError(err), http.StatusConflict)
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
	// Session-path-changing critical sequence: serialize with /resume, /new,
	// and switchModel so the controller and the lease keeper move together.
	// Taken after body decoding so a slow client cannot hold the binding lock.
	s.bindMu.Lock()
	defer s.bindMu.Unlock()
	path, err := s.ctl().ForkNamed(body.Turn, body.Name)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	// The controller switched to the fork (a fresh path); the lease follows it.
	if err := s.rebindSessionLease(s.ctl().SessionPath()); err != nil {
		http.Error(w, sessionInUseError(err), http.StatusConflict)
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

// goal sets or clears the active goal. An empty goal string clears it.
// Setting a non-empty goal disables plan mode (matching the desktop behavior).
func (s *Server) goal(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Goal string `json:"goal"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad body", http.StatusBadRequest)
		return
	}
	goal := strings.TrimSpace(body.Goal)
	if goal == "" {
		s.ctl().ClearGoal()
		w.WriteHeader(http.StatusNoContent)
		return
	}
	// Disable plan mode before setting the goal, mirroring the desktop.
	s.ctl().SetPlanMode(false)
	s.ctl().SetGoal(goal)
	w.WriteHeader(http.StatusNoContent)
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
	dir := s.ctl().SessionDir()
	if dir == "" {
		http.Error(w, "sessions disabled", http.StatusBadRequest)
		return
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		http.Error(w, "invalid session dir", http.StatusBadRequest)
		return
	}
	realDir, err := filepath.EvalSymlinks(absDir)
	if err != nil {
		http.Error(w, "invalid session dir", http.StatusBadRequest)
		return
	}
	absPath, err := filepath.Abs(strings.TrimSpace(body.Path))
	if err != nil || !store.IsSessionTranscriptName(filepath.Base(absPath)) {
		http.Error(w, "invalid session path", http.StatusBadRequest)
		return
	}
	realPath, err := filepath.EvalSymlinks(absPath)
	if err != nil {
		http.Error(w, "invalid session path", http.StatusBadRequest)
		return
	}
	if realPath == realDir || !strings.HasPrefix(realPath, realDir+string(os.PathSeparator)) {
		http.Error(w, "path outside session dir", http.StatusForbidden)
		return
	}
	if agent.IsCleanupPending(realPath) {
		http.Error(w, "session is pending cleanup", http.StatusBadRequest)
		return
	}
	// Session-path-changing critical sequence: two interleaved resumes would
	// leave the controller on one session and the lease on another; serialize
	// with /new, /fork, and switchModel. Taken after body/path validation so a
	// slow client cannot hold the binding lock while uploading.
	s.bindMu.Lock()
	defer s.bindMu.Unlock()
	// Snapshot the current session before switching away — while this process
	// still holds its lease.
	if err := s.ctl().Snapshot(); err != nil {
		slog.Warn("serve: snapshot before resume", "err", err)
	}
	// Refuse to bind a session another runtime is writing (a desktop window,
	// another CLI); on success the lease now guards the resume target.
	if err := s.rebindSessionLease(realPath); err != nil {
		if errors.Is(err, agent.ErrSessionLeaseHeld) {
			http.Error(w, sessionInUseError(err), http.StatusConflict)
		} else {
			http.Error(w, "session lease: "+err.Error(), http.StatusInternalServerError)
		}
		return
	}
	loaded, err := agent.LoadSession(realPath)
	if err != nil {
		// The lease already moved to the target; re-point it at the session the
		// controller still owns (best-effort).
		_ = s.rebindSessionLease(s.ctl().SessionPath())
		http.Error(w, "load session: "+err.Error(), http.StatusBadRequest)
		return
	}
	if hook := resumeBindHookForTest; hook != nil {
		hook()
	}
	s.ctl().Resume(loaded, realPath)
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

// models lists configured chat models for the browser model picker.
func (s *Server) models(w http.ResponseWriter, _ *http.Request) {
	cfg, err := config.Load()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	type modelEntry struct {
		Ref      string `json:"ref"`
		Provider string `json:"provider"`
		Model    string `json:"model"`
		Kind     string `json:"kind,omitempty"`
		Active   bool   `json:"active,omitempty"`
		Default  bool   `json:"default,omitempty"`
	}
	current := currentModelRef(s.ctl())
	label := s.ctl().Label()
	modelCounts := make(map[string]int)
	for i := range cfg.Providers {
		p := &cfg.Providers[i]
		if !p.Configured() {
			continue
		}
		models := p.ChatModelList()
		if len(models) == 0 {
			models = p.ModelList()
		}
		for _, model := range models {
			modelCounts[model]++
		}
	}
	var out []modelEntry
	for i := range cfg.Providers {
		p := &cfg.Providers[i]
		if !p.Configured() {
			continue
		}
		models := p.ChatModelList()
		if len(models) == 0 {
			models = p.ModelList()
		}
		for _, model := range models {
			ref := p.Name + "/" + model
			active := ref == current || p.Name == current
			if !active && current == label && model == label {
				if modelCounts[model] == 1 {
					active = true
				} else {
					active = ref == cfg.DefaultModel
				}
			}
			out = append(out, modelEntry{
				Ref:      ref,
				Provider: p.Name,
				Model:    model,
				Kind:     p.Kind,
				Active:   active,
				Default:  ref == cfg.DefaultModel || p.Name == cfg.DefaultModel,
			})
		}
	}
	if out == nil {
		out = []modelEntry{}
	}
	writeJSON(w, map[string]any{"current": current, "label": label, "default": cfg.DefaultModel, "models": out})
}

func currentModelRef(c control.SessionAPI) string {
	ref := strings.TrimSpace(c.ModelRef())
	if ref != "" {
		return ref
	}
	return strings.TrimSpace(c.Label())
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
		sess["balance"] = map[string]any{
			"display":   b.Display(),
			"available": b.Available,
			"infos":     b.Infos,
		}
	} else if err != nil {
		slog.Warn("serve: balance fetch failed", "err", err)
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
		Temperature: provider.TemperaturePtr(0),
		MaxTokens:   20,
	})
	if err != nil {
		return ""
	}
	var text strings.Builder
	var usage *provider.Usage
	for chunk := range ch {
		switch chunk.Type {
		case provider.ChunkText:
			text.WriteString(chunk.Text)
		case provider.ChunkUsage:
			// Title usage is intentionally not broadcast on the shared chat SSE stream.
		case provider.ChunkError:
			return ""
		}
	}
	if usage != nil && usage.TotalTokens > 0 && s.bc != nil {
		s.bc.Emit(event.Event{Kind: event.Usage, Usage: usage, Pricing: s.titlePrice, UsageSource: event.UsageSourceTitle})
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
		if e.IsDir() || !store.IsSessionTranscriptName(e.Name()) {
			continue
		}
		path := filepath.Join(dir, e.Name())
		if agent.IsCleanupPending(path) {
			continue
		}
		name := strings.TrimSuffix(e.Name(), ".jsonl")
		entry := sessionEntry{Name: name, Path: path, Current: filepath.Clean(path) == current}
		// Event-log aware: reading the .jsonl checkpoint directly would freeze
		// turn counts and titles at the last checkpoint write.
		if first, turns := agent.SessionPreview(path); turns > 0 {
			entry.Turns = turns
			entry.Title = s.sessionTitle(r.Context(), e.Name(), first, agent.SessionContentModTime(path).UnixNano())
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
	if result := finishSessionDestroy(destroy); result.HasTimedOut() {
		if err := agent.MarkCleanupPending(abs, "delete"); err != nil {
			go delayedSessionDelete(absDir, abs, destroy)
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		go delayedSessionDelete(absDir, abs, destroy)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if err := removeSessionFiles(absDir, abs); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func finishSessionDestroy(destroy control.SessionDestroyHandle) jobs.TeardownResult {
	if destroy.Wait != nil {
		result := destroy.Wait()
		if destroy.Finish != nil && !result.HasTimedOut() {
			destroy.Finish()
		}
		return result
	}
	if destroy.Finish != nil {
		destroy.Finish()
	}
	return jobs.TeardownResult{}
}

func delayedSessionDelete(absDir, abs string, destroy control.SessionDestroyHandle) {
	if destroy.WaitAll != nil {
		destroy.WaitAll()
	}
	if err := removeSessionFiles(absDir, abs); err != nil {
		slog.Warn("serve: delayed session delete failed", "path", abs, "err", err)
	}
	if destroy.Finish != nil {
		destroy.Finish()
	}
}

func removeSessionFiles(absDir, abs string) error {
	remove := append([]string{abs}, store.SessionSidecarFiles(abs)...)
	for _, p := range remove {
		if p == "" {
			continue
		}
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	if err := agent.DeleteSubagentsByParent(absDir, agent.BranchID(abs)); err != nil {
		return err
	}
	if err := jobs.RemoveArtifacts(abs); err != nil {
		return err
	}
	return agent.ClearCleanupPending(abs)
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

// todos returns the canonical task list (latest todo_write state merged with
// complete_step advances) so the frontend can render a live task panel.
func (s *Server) todos(w http.ResponseWriter, _ *http.Request) {
	type todoItem struct {
		Content    string `json:"content"`
		Status     string `json:"status"`
		ActiveForm string `json:"activeForm,omitempty"`
		Level      int    `json:"level,omitempty"`
	}
	raw := s.ctl().Todos()
	out := make([]todoItem, len(raw))
	for i, t := range raw {
		out[i] = todoItem{Content: t.Content, Status: t.Status, ActiveForm: t.ActiveForm, Level: t.Level}
	}
	writeJSON(w, out)
}
