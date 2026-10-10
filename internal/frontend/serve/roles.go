// roles.go — which model takes which job.
package serve

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"reasonix/internal/assembly/boot"
	"reasonix/internal/contract/config"
)

// roleFields maps a wire name onto the AgentConfig field that already decides
// the behaviour. A role with no field behind it would be a switch that changes
// nothing, so a name only appears here once the kernel reads it.
var roleFields = map[string]func(*config.Config) *string{
	"planner":  func(c *config.Config) *string { return &c.Agent.PlannerModel },
	"subagent": func(c *config.Config) *string { return &c.Agent.SubagentModel },
	"guardian": func(c *config.Config) *string { return &c.Agent.GuardianModel },
	"vision":   func(c *config.Config) *string { return &c.Agent.VisionModel },
	"decision": func(c *config.Config) *string { return &c.Agent.DecisionModel },
}

// roleAnswers is what a role's model must produce. Decision asks a question
// set a conversation model cannot answer; every other role holds a conversation.
func roleAnswers(role string) config.Answers {
	if strings.TrimSpace(role) == "decision" {
		return config.AnswersDecision
	}
	return config.AnswersChat
}

func (s *Server) registerRoleRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /roles", s.roles)
	mux.HandleFunc("POST /roles", s.setRole)
	mux.HandleFunc("GET /roles/overrides", s.roleOverrides)
	mux.HandleFunc("POST /roles/overrides/clear", s.clearRoleOverride)
}

// roleOverride is a per-profile entry that outranks a role's global model.
// Scope says which file holds it, because only the user's can be cleared here.
type roleOverride struct {
	Key   string `json:"key"`
	Model string `json:"model"`
	Scope string `json:"scope"`
}

// roleOverrides lists, per role, the entries that win over what GET /roles
// reports for it. Only the subagent role has any: its profiles each read their
// own subagent_models key before the global subagent_model.
func (s *Server) roleOverrides(w http.ResponseWriter, _ *http.Request) {
	cfg, err := config.Load()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	user := config.LoadForEdit(config.UserConfigPath()).Agent.SubagentModels
	list := []roleOverride{}
	for _, o := range boot.SubagentModelOverrides(cfg) {
		scope := "project"
		if strings.TrimSpace(user[o.Key]) != "" {
			scope = "user"
		}
		list = append(list, roleOverride{Key: o.Key, Model: o.Model, Scope: scope})
	}
	writeJSON(w, map[string][]roleOverride{"subagent": list})
}

// clearRoleOverride removes one profile's entry, under every spelling the
// profile answers to, from the user config and rebuilds so the global model
// takes effect. It shares setRole's grant.
func (s *Server) clearRoleOverride(w http.ResponseWriter, r *http.Request) {
	if !s.grants.at(r).providerEdit {
		refuse(w, http.StatusForbidden, "roles.editing_disabled", "role editing is not enabled on this server", nil)
		return
	}
	var body struct {
		Role string `json:"role"`
		Key  string `json:"key"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&body); err != nil {
		badBody(w)
		return
	}
	if strings.TrimSpace(body.Role) != "subagent" {
		refuse(w, http.StatusBadRequest, "roles.unknown", "no such role", map[string]any{"role": body.Role})
		return
	}
	unlock := config.LockUserConfigEdits()
	path := config.UserConfigPath()
	edit := config.LoadForEdit(path)
	removed := false
	for _, key := range boot.SubagentModelKeys(body.Key) {
		if _, ok := edit.Agent.SubagentModels[key]; ok {
			delete(edit.Agent.SubagentModels, key)
			removed = true
		}
	}
	if !removed {
		unlock()
		refuse(w, http.StatusConflict, "roles.override_not_in_user_config", "that entry is not in the user config", map[string]any{"key": body.Key})
		return
	}
	err := edit.SaveTo(path)
	unlock()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if err := s.rebuildInPlace(r.Context()); err != nil {
		rebuildFailed(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// An empty ref is the default and means "this job rides the main model". It is
// a real value rather than a missing one, so clearing a role sends "".
func (s *Server) roles(w http.ResponseWriter, _ *http.Request) {
	cfg, err := config.Load()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	out := make(map[string]string, len(roleFields))
	for name, field := range roleFields {
		out[name] = strings.TrimSpace(*field(cfg))
	}
	writeJSON(w, out)
}

// setRole persists one assignment and rebuilds, because boot reads every role
// model while assembling the runtime. It shares the provider-edit grant: both
// write the configuration of the machine running the kernel, which is the one
// thing a server reachable over a network must not let a client do.
func (s *Server) setRole(w http.ResponseWriter, r *http.Request) {
	if !s.grants.at(r).providerEdit {
		refuse(w, http.StatusForbidden, "roles.editing_disabled", "role editing is not enabled on this server", nil)
		return
	}
	var body struct {
		Role string `json:"role"`
		Ref  string `json:"ref"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&body); err != nil {
		badBody(w)
		return
	}
	field, ok := roleFields[strings.TrimSpace(body.Role)]
	if !ok {
		refuse(w, http.StatusBadRequest, "roles.unknown", "no such role", map[string]any{"role": body.Role})
		return
	}
	ref := strings.TrimSpace(body.Ref)
	if ref != "" {
		cfg, err := config.Load()
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
		// Naming a model that does not resolve would strand the role on the next
		// build, and the failure would surface as a broken turn rather than here.
		if !cfg.ModelRefSelectable(ref, s.ctl().ProviderCatalog()) {
			refuse(w, http.StatusBadRequest, "roles.model_unknown", "no configured model matches that reference", map[string]any{"model": ref})
			return
		}
		if err := cfg.RequireAnswers(ref, roleAnswers(body.Role)); err != nil {
			writeErr(w, http.StatusConflict, err)
			return
		}
	}
	edit := config.LoadForEdit(config.UserConfigPath())
	*field(edit) = ref
	if err := edit.SaveTo(config.UserConfigPath()); err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if err := s.rebuildInPlace(r.Context()); err != nil {
		rebuildFailed(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// rebuildInPlace reassembles the runtime on the model it is already using, so a
// setting boot only reads while assembling reaches it. Rebuilding refuses
// mid-turn for the same reason a model switch does: the running loop would be
// swapped underneath.
func (s *Server) rebuildInPlace(ctx context.Context) error {
	ref := currentModelRef(s.ctl())
	if ref == "" {
		return fmt.Errorf("no current model to rebuild on")
	}
	return s.switchModel(ctx, ref)
}
