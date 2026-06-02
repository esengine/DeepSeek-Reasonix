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
	"os"
	"path/filepath"
	"strings"
	"time"

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
	mux.HandleFunc("POST /bypass", s.bypass)
	mux.HandleFunc("POST /answer", s.answer)
	mux.HandleFunc("POST /resume", s.resume)
	mux.HandleFunc("POST /forget", s.forget)
	mux.HandleFunc("GET /checkpoints", s.checkpoints)
	mux.HandleFunc("GET /branches", s.branches)
	mux.HandleFunc("GET /status", s.status)
	mux.HandleFunc("GET /sessions", s.sessions)
	mux.HandleFunc("GET /skills", s.skills)
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
	if err := s.ctrl.Rewind(body.Turn, scope); err != nil {
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
	path, err := s.ctrl.ForkNamed(body.Turn, body.Name)
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
		err = s.ctrl.SummarizeFrom(r.Context(), body.Turn)
	case "upto":
		err = s.ctrl.SummarizeUpTo(r.Context(), body.Turn)
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

// bypass toggles YOLO/bypass mode.
func (s *Server) bypass(w http.ResponseWriter, r *http.Request) {
	var body struct {
		On bool `json:"on"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "bad body", http.StatusBadRequest)
		return
	}
	s.ctrl.SetBypass(body.On)
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
	s.ctrl.AnswerQuestion(body.ID, body.Answers)
	w.WriteHeader(http.StatusNoContent)
}

// resume loads a previous session by index.
func (s *Server) resume(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Path string `json:"path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Path == "" {
		http.Error(w, "missing path", http.StatusBadRequest)
		return
	}
	// Use Submit to handle /resume which the controller dispatches
	s.ctrl.Submit("/resume " + body.Path)
	w.WriteHeader(http.StatusAccepted)
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
	if err := s.ctrl.ForgetMemory(body.Name); err != nil {
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
	raw := s.ctrl.Checkpoints()
	out := make([]cp, len(raw))
	for i, c := range raw {
		out[i] = cp{Turn: c.Turn, Prompt: c.Prompt, Files: len(c.Paths)}
	}
	writeJSON(w, out)
}

// branches returns the branch list and tree text.
func (s *Server) branches(w http.ResponseWriter, _ *http.Request) {
	branches, err := s.ctrl.Branches()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	tree := s.ctrl.BranchTreeText()
	writeJSON(w, map[string]any{"branches": branches, "tree": tree})
}

// status returns a combined status snapshot.
func (s *Server) status(w http.ResponseWriter, r *http.Request) {
	used, window := s.ctrl.ContextSnapshot()
	hit, miss := s.ctrl.SessionCache()
	sess := map[string]any{
		"label":     s.ctrl.Label(),
		"running":   s.ctrl.Running(),
		"plan":      s.ctrl.PlanMode(),
		"bypass":    s.ctrl.Bypass(),
		"cwd":       s.ctrl.SessionDir(),
		"used":      used,
		"window":    window,
		"cacheHit":  hit,
		"cacheMiss": miss,
	}
	if u := s.ctrl.LastUsage(); u != nil {
		sess["lastUsage"] = u
	}
	if b, err := s.ctrl.Balance(r.Context()); err == nil && b != nil {
		sess["balance"] = b
	}
	if j := s.ctrl.Jobs(); len(j) > 0 {
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
	dir := s.ctrl.SessionDir()
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
	current := filepath.Clean(s.ctrl.SessionPath())
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
				first = strings.TrimSpace(m.Content)
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
	raw := s.ctrl.Skills()
	out := make([]skillEntry, len(raw))
	for i, sk := range raw {
		out[i] = skillEntry{Name: sk.Name, Scope: string(sk.Scope), Subagent: sk.RunAs == "subagent", Description: sk.Description}
	}
	writeJSON(w, out)
}
