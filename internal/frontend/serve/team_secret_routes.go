package serve

import (
	"encoding/json"
	"net/http"
	"runtime"

	"reasonix/internal/contract/config"
	"reasonix/internal/safety/sandbox"
	"reasonix/internal/state/teamroster"
)

// secretView is a saved secret as the window may know it: never its value.
type secretView struct {
	Name        string   `json:"name"`
	Purpose     string   `json:"purpose,omitempty"`
	Agents      []string `json:"agents,omitempty"`
	RequestedBy string   `json:"requestedBy,omitempty"`
	UpdatedAt   int64    `json:"updatedAt,omitempty"`
}

// secretRequestView is a request an agent made that the user has not answered.
type secretRequestView struct {
	Name    string `json:"name"`
	Purpose string `json:"purpose,omitempty"`
	Agent   string `json:"agent"`
	At      int64  `json:"at,omitempty"`
}

// retiredView names a value that reaches no command and stays masked.
type retiredView struct {
	Name string `json:"name"`
	At   int64  `json:"at,omitempty"`
}

type secretsView struct {
	Secrets  []secretView        `json:"secrets"`
	Requests []secretRequestView `json:"requests"`
	Retired  []retiredView       `json:"retired"`
	// Guarded says agents' commands here are denied the file the values live in.
	Guarded bool `json:"guarded"`
}

// secretsGuarded is whether an OS sandbox that confines reads is enforced for
// commands on this host. Windows confines writes only, so there it never is.
func secretsGuarded() bool {
	if runtime.GOOS == "windows" {
		return false
	}
	cfg, err := config.Load()
	if err != nil {
		return false
	}
	return cfg.BashMode() == "enforce" && sandbox.AvailableFor(sandbox.Spec{Network: cfg.Sandbox.Network})
}

// The largest body a secret's save carries: the value and a little around it.
const secretBodyLimit = teamroster.MaxSecretBytes + 4<<10

func (t *hubTeam) secrets(w http.ResponseWriter, _ *http.Request) {
	book, err := t.store.SecretMetadata()
	if err != nil {
		teamError(w, err)
		return
	}
	out := secretsView{Secrets: []secretView{}, Requests: []secretRequestView{}, Retired: []retiredView{}, Guarded: secretsGuarded()}
	for _, sc := range book.Secrets {
		out.Secrets = append(out.Secrets, secretView{Name: sc.Name, Purpose: sc.Purpose, Agents: sc.Agents, RequestedBy: sc.RequestedBy, UpdatedAt: sc.UpdatedAt.UnixMilli()})
	}
	for _, q := range book.Requests {
		out.Requests = append(out.Requests, secretRequestView{Name: q.Name, Purpose: q.Purpose, Agent: q.Agent, At: q.At.UnixMilli()})
	}
	for _, r := range book.Retired {
		out.Retired = append(out.Retired, retiredView{Name: r.Name, At: r.At.UnixMilli()})
	}
	writeJSON(w, out)
}

func (t *hubTeam) saveSecret(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, secretBodyLimit)
	var in struct {
		Name      string   `json:"name"`
		Value     string   `json:"value"`
		Purpose   string   `json:"purpose"`
		Agents    []string `json:"agents"`
		Requester string   `json:"requester"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		badBody(w)
		return
	}
	if err := t.service().UserSaveSecret(r.Context(), in.Name, in.Value, in.Purpose, in.Agents, in.Requester); err != nil {
		teamError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (t *hubTeam) cancelSecret(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Agent string `json:"agent"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		badBody(w)
		return
	}
	if err := t.service().UserCancelSecret(r.Context(), r.PathValue("name"), in.Agent); err != nil {
		teamError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (t *hubTeam) forgetRetired(w http.ResponseWriter, r *http.Request) {
	if err := t.service().UserForgetRetired(r.Context()); err != nil {
		teamError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (t *hubTeam) deleteSecret(w http.ResponseWriter, r *http.Request) {
	if err := t.service().UserDeleteSecret(r.Context(), r.PathValue("name")); err != nil {
		teamError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
