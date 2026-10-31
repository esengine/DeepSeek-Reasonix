package serve

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"reasonix/internal/session/team"
	"reasonix/internal/state/teamroster"
)

// registerTeamRoutes serves the team to the window. Every write here is the
// user's own: no agent reaches these routes, so the calls carry the user's
// authority, and a hub that does not hold the team's writer lease refuses them.
func (h *Hub) registerTeamRoutes(mux *http.ServeMux) {
	if h.team == nil {
		return
	}
	t := h.team
	mux.HandleFunc("GET /team", t.getTeam)
	mux.HandleFunc("POST /team/bootstrap", t.writing(t.bootstrap))
	mux.HandleFunc("POST /team/agents", t.writing(t.createAgent))
	mux.HandleFunc("PATCH /team/agents/{id}", t.writing(t.updateAgent))
	mux.HandleFunc("DELETE /team/agents/{id}", t.writing(t.deleteAgent))
	mux.HandleFunc("POST /team/agents/{id}/open", t.writing(t.openAgent))
	mux.HandleFunc("POST /team/agents/{id}/stop", t.writing(t.stopAgent))
	mux.HandleFunc("POST /team/agents/{id}/photo", t.writing(t.setPhoto))
	mux.HandleFunc("DELETE /team/agents/{id}/photo", t.writing(t.clearPhoto))
	mux.HandleFunc("GET /team/avatars/{file}", t.avatar)
	mux.HandleFunc("POST /team/groups", t.writing(t.createGroup))
	mux.HandleFunc("PATCH /team/groups/{id}", t.writing(t.updateGroup))
	mux.HandleFunc("DELETE /team/groups/{id}", t.writing(t.deleteGroup))
	mux.HandleFunc("GET /team/groups/{id}/log", t.groupLog)
	mux.HandleFunc("POST /team/groups/{id}/messages", t.writing(t.postGroup))
	mux.HandleFunc("POST /team/groups/{id}/continue", t.writing(t.continueGroup))
	mux.HandleFunc("POST /team/groups/{id}/members/{agent}/stop", t.writing(t.stopMember))
	mux.HandleFunc("POST /team/groups/{id}/members/{agent}/open", t.writing(t.openMember))
	mux.HandleFunc("GET /team/exchanges", t.exchanges)
	mux.HandleFunc("GET /team/memory", t.sharedMemory)
	mux.HandleFunc("PATCH /team/prefs", t.writing(t.setPrefs))
	mux.HandleFunc("GET /team/secrets", t.secrets)
	mux.HandleFunc("POST /team/secrets", t.writing(t.saveSecret))
	mux.HandleFunc("POST /team/secrets/{name}/cancel", t.writing(t.cancelSecret))
	mux.HandleFunc("DELETE /team/secrets/{name}", t.writing(t.deleteSecret))
	mux.HandleFunc("DELETE /team/secrets-retired", t.writing(t.forgetRetired))
	mux.HandleFunc("GET /team/schedules", t.schedules)
	mux.HandleFunc("POST /team/schedules", t.writing(t.saveSchedule))
	mux.HandleFunc("PATCH /team/schedules/{id}", t.writing(t.saveSchedule))
	mux.HandleFunc("DELETE /team/schedules/{id}", t.writing(t.deleteSchedule))
	mux.HandleFunc("DELETE /team/memory/{tier}/{name}", t.writing(t.forgetShared))
}

// writing refuses a write when another process holds the team.
func (t *hubTeam) writing(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if refuseDevice(w, r) {
			return
		}
		if t.service() == nil {
			refuse(w, http.StatusConflict, "team.read_only", "the team is open for writing in another Reasonix window", nil)
			return
		}
		next(w, r)
	}
}

// teamError gives a team error its status and code. Domain errors are the
// request's; a conflict is the roster moving under it; anything the team does
// not name is the host's own failure, and says so.
func teamError(w http.ResponseWriter, err error) {
	status, code := http.StatusInternalServerError, "team.store_failed"
	for _, v := range []struct {
		err    error
		status int
		code   string
	}{
		{team.ErrUnsupportedPlatform, http.StatusServiceUnavailable, "team.disabled"},
		{team.ErrConfigurationApproval, http.StatusForbidden, "team.approval_required"},
		{teamroster.ErrAgentCapacity, http.StatusConflict, "team.limit"},
		{teamroster.ErrGroupCapacity, http.StatusConflict, "team.limit"},
		{errTeamFolderUntrusted, http.StatusForbidden, "team.folder_untrusted"},
		{team.ErrRateLimited, http.StatusTooManyRequests, "team.rate_limited"},
		{team.ErrDepth, http.StatusBadRequest, "team.depth"},
		{team.ErrOpenLimit, http.StatusConflict, "team.limit"},
		{team.ErrCreateLimit, http.StatusConflict, "team.limit"},
		{team.ErrNotMain, http.StatusForbidden, "team.not_main"},
		{team.ErrSelfOnly, http.StatusForbidden, "team.self_only"},
		{team.ErrNotAdmin, http.StatusForbidden, "team.not_admin"},
		{team.ErrNotInGroup, http.StatusForbidden, "team.not_in_group"},
		{team.ErrNotYourTurn, http.StatusConflict, "team.not_your_turn"},
		{team.ErrHandSize, http.StatusBadRequest, "team.invalid"},
		{team.ErrSelf, http.StatusBadRequest, "team.self"},
		{team.ErrUpstream, http.StatusBadRequest, "team.upstream"},
		{teamroster.ErrRequestReused, http.StatusConflict, "team.request_reused"},
		{teamroster.ErrSecretCredentials, http.StatusServiceUnavailable, "team.credentials_unavailable"},
		{teamroster.ErrUnknownAgent, http.StatusNotFound, "team.unknown_agent"},
		{teamroster.ErrUnknownGroup, http.StatusNotFound, "team.unknown_group"},
		{teamroster.ErrUnknownSchedule, http.StatusNotFound, "team.unknown_schedule"},
		{teamroster.ErrUnknownSecret, http.StatusNotFound, "team.unknown_secret"},
		{teamroster.ErrNotMember, http.StatusBadRequest, "team.not_member"},
		{teamroster.ErrGroupSize, http.StatusBadRequest, "team.group_size"},
		{teamroster.ErrNameTaken, http.StatusBadRequest, "team.name_taken"},
		{teamroster.ErrLastMain, http.StatusBadRequest, "team.last_main"},
		{teamroster.ErrTooLong, http.StatusBadRequest, "team.too_long"},
		{teamroster.ErrEmpty, http.StatusBadRequest, "team.invalid"},
		{teamroster.ErrInvalid, http.StatusBadRequest, "team.invalid"},
		{teamroster.ErrConflict, http.StatusConflict, "team.conflict"},
		{teamroster.ErrReadOnly, http.StatusConflict, "team.read_only"},
		{teamroster.ErrNewerSchema, http.StatusConflict, "team.newer_schema"},
		{team.ErrEmptyMessage, http.StatusBadRequest, "team.invalid"},
		{team.ErrMessageTooLong, http.StatusBadRequest, "team.too_long"},
		{team.ErrArchived, http.StatusConflict, "team.archived"},
		{team.ErrUnavailable, http.StatusServiceUnavailable, "team.unavailable"},
	} {
		if errors.Is(err, v.err) {
			status, code = v.status, v.code
			break
		}
	}
	var params map[string]any
	var fe *teamroster.FieldError
	if errors.As(err, &fe) {
		params = map[string]any{"field": fe.Field}
	}
	refuse(w, status, code, err.Error(), params)
}

func (t *hubTeam) writeView(w http.ResponseWriter) {
	v, err := t.view()
	if err != nil {
		teamError(w, err)
		return
	}
	writeJSON(w, v)
}

func (t *hubTeam) getTeam(w http.ResponseWriter, _ *http.Request) { t.writeView(w) }

func (t *hubTeam) bootstrap(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name string `json:"name"`
		Role string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || in.Name == "" {
		badBody(w)
		return
	}
	if _, err := t.service().Bootstrap(r.Context(), in.Name, in.Role); err != nil {
		teamError(w, err)
		return
	}
	t.writeView(w)
}

// agentBody is what the window sends to create or change an agent; an absent
// field is left as it is.
type agentBody struct {
	Name         *string         `json:"name"`
	Role         *string         `json:"role"`
	Instructions *string         `json:"instructions"`
	Avatar       *teamAvatarView `json:"avatar"`
	Model        *string         `json:"model"`
	Folder       *string         `json:"folder"`
	Permission   *string         `json:"permission"`
	Main         *bool           `json:"main"`
	Pinned       *bool           `json:"pinned"`
	Hidden       *bool           `json:"hidden"`
}

func (b agentBody) change() team.UserAgentChange {
	ch := team.UserAgentChange{
		AgentChange: team.AgentChange{Name: b.Name, Role: b.Role, Instructions: b.Instructions, Model: b.Model, Folder: b.Folder},
		Permission:  b.Permission, Main: b.Main, Pinned: b.Pinned, Hidden: b.Hidden,
	}
	if b.Avatar != nil {
		ch.Avatar = &teamroster.Avatar{Glyph: b.Avatar.Glyph, Color: b.Avatar.Color}
	}
	return ch
}

func (t *hubTeam) createAgent(w http.ResponseWriter, r *http.Request) {
	var in agentBody
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		badBody(w)
		return
	}
	if _, err := t.service().UserCreateAgent(r.Context(), in.change()); err != nil {
		teamError(w, err)
		return
	}
	t.writeView(w)
}

func (t *hubTeam) updateAgent(w http.ResponseWriter, r *http.Request) {
	var in agentBody
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		badBody(w)
		return
	}
	id := r.PathValue("id")
	a, err := t.service().UserUpdateAgent(r.Context(), id, in.change())
	if err != nil {
		teamError(w, err)
		return
	}
	if in.Permission != nil {
		t.setPermission(id, a.Permission)
	}
	t.writeView(w)
}

func (t *hubTeam) deleteAgent(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := t.service().UserDeleteAgent(r.Context(), id); err != nil {
		teamError(w, err)
		return
	}
	t.closeAgent(id)
	t.writeView(w)
}

// closeAgent closes every open session of an agent that left the team.
func (t *hubTeam) closeAgent(agent string) {
	t.mu.Lock()
	var ids []string
	for seat, id := range t.seats {
		if seat.Agent == agent {
			ids = append(ids, id)
		}
	}
	t.mu.Unlock()
	for _, id := range ids {
		_ = t.h.closeRuntimeNow(id)
	}
}

// openSeat opens a seat for the window and answers the pane it is.
func (t *hubTeam) openSeat(w http.ResponseWriter, r *http.Request, seat team.SeatRef) {
	pane, err := t.Ensure(r.Context(), seat)
	if err != nil {
		teamError(w, err)
		return
	}
	rt := pane.(runtimePane).rt
	t.holdForWindow(rt.ID)
	writeJSON(w, t.h.viewOf(rt))
}

func (t *hubTeam) openAgent(w http.ResponseWriter, r *http.Request) {
	t.openSeat(w, r, team.SeatRef{Agent: r.PathValue("id")})
}

func (t *hubTeam) openMember(w http.ResponseWriter, r *http.Request) {
	t.openSeat(w, r, team.SeatRef{Agent: r.PathValue("agent"), Group: r.PathValue("id")})
}

func (t *hubTeam) stopAgent(w http.ResponseWriter, r *http.Request) {
	t.service().Stop(r.PathValue("id"))
	w.WriteHeader(http.StatusNoContent)
}

// groupBody is what the window sends to create or change a group.
type groupBody struct {
	Name       *string  `json:"name"`
	Notice     *string  `json:"notice"`
	Members    []string `json:"members"`
	Admin      *string  `json:"admin"`
	Mode       *string  `json:"mode"`
	RelayLimit *int     `json:"relayLimit"`
	Folder     *string  `json:"folder"`
	Pinned     *bool    `json:"pinned"`
	Hidden     *bool    `json:"hidden"`
}

func (b groupBody) change() team.UserGroupChange {
	ch := team.UserGroupChange{Name: b.Name, Notice: b.Notice, Members: b.Members, Admin: b.Admin, RelayLimit: b.RelayLimit, Folder: b.Folder, Pinned: b.Pinned, Hidden: b.Hidden}
	if b.Mode != nil {
		m := teamroster.Mode(*b.Mode)
		ch.Mode = &m
	}
	return ch
}

func (t *hubTeam) createGroup(w http.ResponseWriter, r *http.Request) {
	var in groupBody
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		badBody(w)
		return
	}
	if _, err := t.service().UserCreateGroup(r.Context(), in.change()); err != nil {
		teamError(w, err)
		return
	}
	t.writeView(w)
}

func (t *hubTeam) updateGroup(w http.ResponseWriter, r *http.Request) {
	var in groupBody
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		badBody(w)
		return
	}
	if _, err := t.service().UserUpdateGroup(r.Context(), r.PathValue("id"), in.change()); err != nil {
		teamError(w, err)
		return
	}
	t.writeView(w)
}

func (t *hubTeam) deleteGroup(w http.ResponseWriter, r *http.Request) {
	if err := t.service().UserDeleteGroup(r.Context(), r.PathValue("id")); err != nil {
		teamError(w, err)
		return
	}
	t.writeView(w)
}

// groupLog answers a page of a group's log: the lines after ?after=, or the
// last ?limit= lines before ?before=.
func (t *hubTeam) groupLog(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	after, _ := strconv.ParseInt(q.Get("after"), 10, 64)
	before, _ := strconv.ParseInt(q.Get("before"), 10, 64)
	limit, _ := strconv.Atoi(q.Get("limit"))
	var lines []teamroster.GroupLine
	var err error
	if before > 0 || (after == 0 && limit > 0) {
		lines, err = t.store.LinesBefore(r.PathValue("id"), before, limit)
	} else {
		lines, err = t.store.Lines(r.PathValue("id"), after, 0)
	}
	if err != nil {
		teamError(w, err)
		return
	}
	writeJSON(w, lines)
}

func (t *hubTeam) postGroup(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Text     string   `json:"text"`
		Mentions []string `json:"mentions"`
		Everyone bool     `json:"everyone"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		badBody(w)
		return
	}
	line, err := t.service().UserPost(r.Context(), r.PathValue("id"), in.Text, in.Mentions, in.Everyone)
	if err != nil {
		teamError(w, err)
		return
	}
	writeJSON(w, line)
}

func (t *hubTeam) continueGroup(w http.ResponseWriter, r *http.Request) {
	if err := t.service().Continue(r.Context(), r.PathValue("id")); err != nil {
		teamError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (t *hubTeam) stopMember(w http.ResponseWriter, r *http.Request) {
	if err := t.service().StopMember(r.Context(), r.PathValue("id"), r.PathValue("agent")); err != nil {
		teamError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// exchanges answers the messages an agent sent or received, newest last.
func (t *hubTeam) exchanges(w http.ResponseWriter, r *http.Request) {
	agent := r.URL.Query().Get("agent")
	book, err := t.store.Exchanges()
	if err != nil {
		teamError(w, err)
		return
	}
	out := []teamroster.Exchange{}
	for _, x := range book {
		if agent == "" || x.From == agent || x.To == agent {
			out = append(out, x)
		}
	}
	writeJSON(w, out)
}
