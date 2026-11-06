package serve

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"

	"reasonix/internal/platform/terminal"
)

const (
	codeTerminalNotFound = "terminal.not_found"
	codeTerminalShell    = "terminal.shell_missing"
	codeTerminalStart    = "terminal.start_failed"
	codeTerminalExited   = "terminal.exited"
	codeTerminalLimit    = "terminal.limit"
	terminalInputLimit   = 1 << 16
)

// registerWorkbenchRoutes is what the panes beside the conversation call on.
func (s *Server) registerWorkbenchRoutes(mux *http.ServeMux) {
	s.registerBrowserRoutes(mux)
	s.registerTerminalRoutes(mux)
}

func (s *Server) registerTerminalRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /terminals/shells", s.terminalShells)
	mux.HandleFunc("GET /terminals", s.terminalList)
	mux.HandleFunc("POST /terminals", s.terminalOpen)
	mux.HandleFunc("DELETE /terminals/{id}", s.terminalClose)
	mux.HandleFunc("POST /terminals/{id}/restart", s.terminalRestart)
	mux.HandleFunc("POST /terminals/{id}/resize", s.terminalResize)
	mux.HandleFunc("POST /terminals/{id}/input", s.terminalInput)
	mux.HandleFunc("GET /terminals/{id}/stream", s.terminalStream)
}

// refuseTerminal names a terminal failure by the code its producer gave it.
func refuseTerminal(w http.ResponseWriter, err error) {
	switch terminal.CodeOf(err) {
	case terminal.CodeNotFound:
		refuse(w, http.StatusNotFound, codeTerminalNotFound, err.Error(), nil)
	case terminal.CodeShellMissing:
		refuse(w, http.StatusBadRequest, codeTerminalShell, err.Error(), nil)
	case terminal.CodeExited:
		refuse(w, http.StatusConflict, codeTerminalExited, err.Error(), nil)
	case terminal.CodeLimit:
		refuse(w, http.StatusConflict, codeTerminalLimit, err.Error(), nil)
	default:
		refuse(w, http.StatusInternalServerError, codeTerminalStart, err.Error(), nil)
	}
}

func (s *Server) terminalShells(w http.ResponseWriter, r *http.Request) {
	if refuseDevice(w, r) {
		return
	}
	writeJSON(w, terminal.Shells(os.Getenv))
}

func (s *Server) terminalList(w http.ResponseWriter, r *http.Request) {
	if refuseDevice(w, r) {
		return
	}
	writeJSON(w, s.terminals.List())
}

// terminalOpen starts the shell the person picked, or their best one, in the
// workspace folder.
func (s *Server) terminalOpen(w http.ResponseWriter, r *http.Request) {
	if refuseDevice(w, r) {
		return
	}
	var req struct {
		Shell string `json:"shell"`
		Cols  uint16 `json:"cols"`
		Rows  uint16 `json:"rows"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, terminalInputLimit)).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		badBody(w)
		return
	}
	var sh *terminal.Shell
	for _, c := range terminal.Shells(os.Getenv) {
		if req.Shell == "" || c.Kind == req.Shell {
			sh = &c
			break
		}
	}
	if sh == nil {
		refuse(w, http.StatusBadRequest, codeTerminalShell, "that shell is not on this machine", nil)
		return
	}
	sess, err := s.terminals.Open(terminal.Options{
		Shell: sh.Path, Args: sh.Args, Name: sh.Name,
		Dir: s.ctl().WorkspaceRoot(), Env: terminal.PersonEnv(os.Environ()),
		Cols: req.Cols, Rows: req.Rows,
	})
	if err != nil {
		refuseTerminal(w, err)
		return
	}
	writeJSON(w, sess.Info())
}

func (s *Server) terminalClose(w http.ResponseWriter, r *http.Request) {
	if refuseDevice(w, r) {
		return
	}
	if err := s.terminals.Close(r.PathValue("id")); err != nil {
		refuseTerminal(w, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

func (s *Server) terminalRestart(w http.ResponseWriter, r *http.Request) {
	if refuseDevice(w, r) {
		return
	}
	sess, err := s.terminals.Get(r.PathValue("id"))
	if err == nil {
		err = sess.Restart()
	}
	if err != nil {
		refuseTerminal(w, err)
		return
	}
	writeJSON(w, sess.Info())
}

func (s *Server) terminalResize(w http.ResponseWriter, r *http.Request) {
	if refuseDevice(w, r) {
		return
	}
	var req struct {
		Cols uint16 `json:"cols"`
		Rows uint16 `json:"rows"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<10)).Decode(&req); err != nil {
		badBody(w)
		return
	}
	sess, err := s.terminals.Get(r.PathValue("id"))
	if err == nil {
		err = sess.Resize(req.Cols, req.Rows)
	}
	if err != nil {
		refuseTerminal(w, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// terminalInput carries the person's keystrokes as they typed them.
func (s *Server) terminalInput(w http.ResponseWriter, r *http.Request) {
	if refuseDevice(w, r) {
		return
	}
	var req struct {
		Data string `json:"data"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, terminalInputLimit)).Decode(&req); err != nil {
		badBody(w)
		return
	}
	sess, err := s.terminals.Get(r.PathValue("id"))
	if err == nil {
		err = sess.Write([]byte(req.Data))
	}
	if err != nil {
		refuseTerminal(w, err)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

type terminalFrame struct {
	Kind string `json:"kind"` // replay | out | exit
	Data string `json:"data,omitempty"`
	Code *int   `json:"code,omitempty"`
}

// terminalStream sends what the shell held, then what it writes, as base64 so
// bytes that are not text cross intact, and ends with the exit code.
func (s *Server) terminalStream(w http.ResponseWriter, r *http.Request) {
	if refuseDevice(w, r) {
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		refuse(w, http.StatusInternalServerError, "stream.unsupported", "this transport cannot stream", nil)
		return
	}
	sess, err := s.terminals.Get(r.PathValue("id"))
	if err != nil {
		refuseTerminal(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	send := func(f terminalFrame) {
		b, _ := json.Marshal(f)
		fmt.Fprintf(w, "data: %s\n\n", b)
		flusher.Flush()
	}
	sub := sess.Subscribe()
	defer sub.Cancel()
	send(terminalFrame{Kind: "replay", Data: base64.StdEncoding.EncodeToString(sub.Replay)})
	for {
		select {
		case c, open := <-sub.Output:
			if !open {
				<-sess.Done()
				send(terminalFrame{Kind: "exit", Code: sess.Info().ExitCode})
				return
			}
			send(terminalFrame{Kind: "out", Data: base64.StdEncoding.EncodeToString(c)})
		case <-r.Context().Done():
			return
		}
	}
}
