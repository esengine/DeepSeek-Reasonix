// boundary.go — the tool boundary as HTTP: the approval rules, and the jail an
// approved call runs inside.
package serve

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"reasonix/internal/safety/permission"
	"reasonix/internal/session/control"
)

func (s *Server) registerBoundaryRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /permissions", s.permissions)
	mux.HandleFunc("POST /permissions", s.savePermissions)
	mux.HandleFunc("POST /permissions/revoke", s.revokeSessionGrant)
	mux.HandleFunc("POST /permissions/remembered/revoke", s.revokeRememberedProjectRule)
	mux.HandleFunc("GET /sandbox", s.sandboxSettings)
	mux.HandleFunc("GET /workspace/roots", s.workspaceRoots)
	mux.HandleFunc("POST /workspace/roots", s.addWorkspaceRoot)
	mux.HandleFunc("POST /workspace/roots/remove", s.removeWorkspaceRoot)
	mux.HandleFunc("POST /sandbox", s.saveSandboxSettings)
	mux.HandleFunc("GET /browser-tools", s.browserToolsSettings)
	mux.HandleFunc("POST /browser-tools", s.saveBrowserToolsSettings)
	mux.HandleFunc("GET /present-files", s.presentFilesSettings)
	mux.HandleFunc("POST /present-files", s.savePresentFilesSettings)
	mux.HandleFunc("GET /subagent-claims", s.subagentClaimsSettings)
	mux.HandleFunc("POST /subagent-claims", s.saveSubagentClaimsSettings)
	mux.HandleFunc("GET /subagent-steer-limits", s.subagentSteerSettings)
	mux.HandleFunc("POST /subagent-steer-limits", s.saveSubagentSteerSettings)
	mux.HandleFunc("GET /write-lease", s.writeLeaseSettings)
	mux.HandleFunc("POST /write-lease", s.saveWriteLeaseSettings)
	mux.HandleFunc("GET /plan-approval", s.planApprovalSettings)
	mux.HandleFunc("POST /plan-approval", s.savePlanApprovalSettings)
	mux.HandleFunc("GET /default-mode", s.defaultModeSettings)
	mux.HandleFunc("POST /default-mode", s.saveDefaultModeSettings)
	mux.HandleFunc("GET /tools", s.toolSettings)
	mux.HandleFunc("POST /tools/disabled", s.saveToolDisabled)
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
		var unknown *permission.UnknownToolError
		if errors.As(err, &unknown) {
			refuse(w, http.StatusBadRequest, "permissions.rule_unknown_tool", err.Error(),
				map[string]any{"list": unknown.List, "rule": unknown.Rule, "tool": unknown.Tool})
			return
		}
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

// A remembered project approval is read by boot when it builds the permission
// gate. Serialize its removal with rebuilds and refuse while a turn is active.
func (s *Server) revokeRememberedProjectRule(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Rule string `json:"rule"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&body); err != nil {
		badBody(w)
		return
	}
	if strings.TrimSpace(body.Rule) == "" {
		refuse(w, http.StatusBadRequest, "permissions.rule_required", "rule is required", nil)
		return
	}
	s.bindMu.Lock()
	defer s.bindMu.Unlock()
	ctrl := s.ctl()
	if controllerHasActiveRuntimeWork(ctrl) {
		refuse(w, http.StatusConflict, "permissions.busy", "finish the current work before revoking a remembered rule", nil)
		return
	}
	model := currentModelRef(ctrl)
	if model == "" {
		refuse(w, http.StatusConflict, "permissions.rebuild_unavailable", "no current model to rebuild", nil)
		return
	}
	if err := ctrl.RevokeRememberedProjectRule(body.Rule); err != nil {
		refuse(w, http.StatusConflict, "project_grants.unavailable", "remembered project rules could not be updated", nil)
		return
	}
	if err := s.switchModelLocked(r.Context(), model); err != nil {
		rebuildFailed(w, err)
		return
	}
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
		var refusal *control.WorkspaceRootError
		if errors.As(err, &refusal) {
			refuse(w, http.StatusBadRequest, string(refusal.Code), "this folder cannot be granted write access", nil)
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
		// A boolean cannot be a bad request: what is left is the file or the disk.
		saveFailed(w, http.StatusInternalServerError, "browser_tools.save_failed", err)
		return
	}
	if err := s.rebuildInPlace(r.Context()); err != nil {
		rebuildFailed(w, err)
		return
	}
	writeJSON(w, s.ctl().BrowserToolsSettings())
}

func (s *Server) subagentClaimsSettings(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, s.ctl().SubagentClaimsSettings())
}

// saveSubagentClaimsSettings rides the provider-edit grant: choosing optimistic
// claims lets sub-agents write beside each other without a fence, so a networked
// client must not make that choice for the machine's owner.
func (s *Server) saveSubagentClaimsSettings(w http.ResponseWriter, r *http.Request) {
	if !s.grants.at(r).providerEdit {
		refuse(w, http.StatusForbidden, "subagent_claims.editing_disabled", "sub-agent claim editing is not enabled on this server", nil)
		return
	}
	var body struct {
		Optimistic *bool `json:"optimistic"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&body); err != nil {
		badBody(w)
		return
	}
	if body.Optimistic == nil {
		refuse(w, http.StatusBadRequest, "subagent_claims.no_choice", "optimistic is required", nil)
		return
	}
	if err := s.ctl().SaveSubagentClaimsSettings(*body.Optimistic); err != nil {
		saveFailed(w, http.StatusInternalServerError, "subagent_claims.save_failed", err)
		return
	}
	if err := s.rebuildInPlace(r.Context()); err != nil {
		rebuildFailed(w, err)
		return
	}
	writeJSON(w, s.ctl().SubagentClaimsSettings())
}

func (s *Server) writeLeaseSettings(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, s.ctl().WriteLeaseSettings())
}

// saveWriteLeaseSettings rides the provider-edit grant: choosing optimistic
// lets two sessions write one file with the last write winning, so a networked
// client must not make that choice for the machine's owner. It does not rebuild
// the runtime: a rebuild adopts the lease it already holds, so the choice is for
// the sessions opened afterwards and the answer says so by not pretending.
func (s *Server) saveWriteLeaseSettings(w http.ResponseWriter, r *http.Request) {
	if !s.grants.at(r).providerEdit {
		refuse(w, http.StatusForbidden, "write_lease.editing_disabled", "write-lease editing is not enabled on this server", nil)
		return
	}
	var body struct {
		Optimistic     *bool `json:"optimistic"`
		MaxWaitMinutes *int  `json:"maxWaitMinutes"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&body); err != nil {
		badBody(w)
		return
	}
	if body.Optimistic == nil && body.MaxWaitMinutes == nil {
		refuse(w, http.StatusBadRequest, "write_lease.no_choice", "optimistic or maxWaitMinutes is required", nil)
		return
	}
	if body.MaxWaitMinutes != nil && *body.MaxWaitMinutes < 0 {
		refuse(w, http.StatusBadRequest, "write_lease.bad_wait", "maxWaitMinutes cannot be negative", nil)
		return
	}
	if err := s.ctl().SaveWriteLeaseSettings(body.Optimistic, body.MaxWaitMinutes); err != nil {
		saveFailed(w, http.StatusInternalServerError, "write_lease.save_failed", err)
		return
	}
	writeJSON(w, s.ctl().WriteLeaseSettings())
}

func (s *Server) subagentSteerSettings(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, s.ctl().SubagentSteerSettings())
}

// saveSubagentSteerSettings rides the provider-edit grant: raising or lifting a
// limit on what can be said to a running sub-agent is the machine owner's call,
// not a networked client's.
func (s *Server) saveSubagentSteerSettings(w http.ResponseWriter, r *http.Request) {
	if !s.grants.at(r).providerEdit {
		refuse(w, http.StatusForbidden, "subagent_steer.editing_disabled", "sub-agent guidance limit editing is not enabled on this server", nil)
		return
	}
	var body struct {
		MaxBytes  *int `json:"maxBytes"`
		PerMinute *int `json:"perMinute"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&body); err != nil {
		badBody(w)
		return
	}
	for field, v := range map[string]*int{"maxBytes": body.MaxBytes, "perMinute": body.PerMinute} {
		if v == nil || *v < 0 {
			missingField(w, field)
			return
		}
	}
	if err := s.ctl().SaveSubagentSteerSettings(*body.MaxBytes, *body.PerMinute); err != nil {
		saveFailed(w, http.StatusInternalServerError, "subagent_steer.save_failed", err)
		return
	}
	writeJSON(w, s.ctl().SubagentSteerSettings())
}
