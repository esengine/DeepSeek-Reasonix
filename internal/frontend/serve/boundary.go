// boundary.go — the tool boundary as HTTP: the approval rules, and the jail an
// approved call runs inside.
package serve

import (
	"encoding/json"
	"errors"
	"net/http"

	"reasonix/internal/session/control"
)

func (s *Server) registerBoundaryRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /permissions", s.permissions)
	mux.HandleFunc("POST /permissions", s.savePermissions)
	mux.HandleFunc("POST /permissions/revoke", s.revokeSessionGrant)
	mux.HandleFunc("GET /sandbox", s.sandboxSettings)
	mux.HandleFunc("POST /sandbox", s.saveSandboxSettings)
	mux.HandleFunc("GET /browser-tools", s.browserToolsSettings)
	mux.HandleFunc("POST /browser-tools", s.saveBrowserToolsSettings)
	// The file every one of these is written to, for when it is the thing that
	// is wrong: each save above refuses with the same code, and this is where a
	// surface reads it before trying, and repairs it after.
	s.registerConfigFileRoutes(mux)
}

func (s *Server) permissions(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, s.ctl().PermissionRules())
}

// savePermissions writes the deny/ask/allow lists and rebuilds. These decide
// what the agent may do to the files of the machine running the kernel, so they
// ride the same grant as the other host-owned settings: a networked client must
// not be able to widen its own boundary.
func (s *Server) savePermissions(w http.ResponseWriter, r *http.Request) {
	if !s.grants.at(r).providerEdit {
		refuse(w, http.StatusForbidden, "permissions.editing_disabled", "permission editing is not enabled on this server", nil)
		return
	}
	var body control.PermissionLists
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&body); err != nil {
		badBody(w)
		return
	}
	if err := s.ctl().SavePermissionRules(body); err != nil {
		saveFailed(w, http.StatusBadRequest, "permissions.rejected", err)
		return
	}
	if err := s.rebuildInPlace(r.Context()); err != nil {
		rebuildFailed(w, err)
		return
	}
	writeJSON(w, s.ctl().PermissionRules())
}

// revokeSessionGrant takes back what a prompt allowed for this session. It is
// not editing the file, so it is not behind the provider-edit grant: taking
// permission away is always the reader's to do.
func (s *Server) revokeSessionGrant(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Rule string `json:"rule"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&body); err != nil {
		badBody(w)
		return
	}
	s.ctl().RevokeSessionGrant(body.Rule)
	writeJSON(w, s.ctl().PermissionRules())
}

func (s *Server) sandboxSettings(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, s.ctl().SandboxSettings())
}

func (s *Server) saveSandboxSettings(w http.ResponseWriter, r *http.Request) {
	if !s.grants.at(r).providerEdit {
		refuse(w, http.StatusForbidden, "sandbox.editing_disabled", "sandbox editing is not enabled on this server", nil)
		return
	}
	var body control.SandboxSettings
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 32<<10)).Decode(&body); err != nil {
		badBody(w)
		return
	}
	if err := s.ctl().SaveSandboxSettings(body); err != nil {
		// Not a bad request and not an unwritable file: the mode is real and this
		// host cannot run it. GET /sandbox already says which backend is missing,
		// so this only has to carry that the write did not happen.
		if errors.Is(err, control.ErrSandboxUnavailable) {
			refuse(w, http.StatusConflict, "sandbox.unavailable", err.Error(), nil)
			return
		}
		saveFailed(w, http.StatusBadRequest, "sandbox.rejected", err)
		return
	}
	if err := s.rebuildInPlace(r.Context()); err != nil {
		rebuildFailed(w, err)
		return
	}
	writeJSON(w, s.ctl().SandboxSettings())
}

func (s *Server) browserToolsSettings(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, s.ctl().BrowserToolsSettings())
}

// saveBrowserToolsSettings rides the provider-edit grant: switching the tools on
// hands the agent a browser that reaches the network from this machine.
func (s *Server) saveBrowserToolsSettings(w http.ResponseWriter, r *http.Request) {
	if !s.grants.at(r).providerEdit {
		refuse(w, http.StatusForbidden, "browser_tools.editing_disabled", "browser tool editing is not enabled on this server", nil)
		return
	}
	var body struct {
		Enabled *bool `json:"enabled"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&body); err != nil {
		badBody(w)
		return
	}
	if body.Enabled == nil {
		refuse(w, http.StatusBadRequest, "browser_tools.no_enabled", "enabled is required", nil)
		return
	}
	if err := s.ctl().SaveBrowserToolsSettings(*body.Enabled); err != nil {
		saveFailed(w, http.StatusBadRequest, "browser_tools.rejected", err)
		return
	}
	if err := s.rebuildInPlace(r.Context()); err != nil {
		rebuildFailed(w, err)
		return
	}
	writeJSON(w, s.ctl().BrowserToolsSettings())
}
