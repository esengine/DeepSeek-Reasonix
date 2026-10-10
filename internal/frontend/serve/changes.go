package serve

import (
	"encoding/json"
	"errors"
	"net/http"

	"reasonix/internal/platform/gitcmd"
	"reasonix/internal/platform/gitstatus"
	"reasonix/internal/session/control"
)

// changes reports what the working tree currently differs by. A frontend that
// derives its pending-change list from tool events instead keeps showing a file
// the agent created and a shell command then deleted, because no event says so.
// Repo is false for a workspace that is not version-controlled, which is a
// fallback signal and not a failure.
func (s *Server) changes(w http.ResponseWriter, r *http.Request) {
	list, ok, err := gitstatus.Status(r.Context(), workspaceRepo(s.ctl()))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	if list == nil {
		list = []gitstatus.Change{}
	}
	w.Header().Set("content-type", "application/json")
	_ = json.NewEncoder(w).Encode(struct {
		Repo    bool               `json:"repo"`
		Changes []gitstatus.Change `json:"changes"`
	}{Repo: ok, Changes: list})
}

type workspaceGitView struct {
	Repo      bool   `json:"repo"`
	Name      string `json:"name"`
	Branch    string `json:"branch"`
	Detached  bool   `json:"detached"`
	Added     int    `json:"added"`
	Removed   int    `json:"removed"`
	Untracked int    `json:"untracked"`
}

// workspaceGit answers with the work tree's one-line identity. Repo is false
// for a workspace that is not version-controlled, as for /changes.
func (s *Server) workspaceGit(w http.ResponseWriter, r *http.Request) {
	info, ok := gitstatus.Summary(r.Context(), workspaceRepo(s.ctl()))
	w.Header().Set("content-type", "application/json")
	_ = json.NewEncoder(w).Encode(workspaceGitView{
		Repo: ok, Name: info.Name, Branch: info.Branch, Detached: info.Detached,
		Added: info.Added, Removed: info.Removed, Untracked: info.Untracked,
	})
}

// changeDiff answers with one path's working-tree diff, so a reader can see a
// change without leaving the window for an editor. The path is a query
// parameter a client supplies, and gitstatus.Diff is what keeps it inside the
// tree — a refusal here is a bad request, not a broken repository, and the two
// carry different codes because a frontend has to say different things.
func (s *Server) changeDiff(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Query().Get("path")
	text, truncated, err := gitstatus.Diff(r.Context(), workspaceRepo(s.ctl()), path)
	if errors.Is(err, gitstatus.ErrPathOutsideTree) {
		refuse(w, http.StatusBadRequest, "changes.path_outside_tree",
			"that path is not inside the working tree", map[string]any{"path": path})
		return
	}
	if err != nil {
		refuse(w, http.StatusInternalServerError, "changes.diff_failed", err.Error(), nil)
		return
	}
	w.Header().Set("content-type", "application/json")
	_ = json.NewEncoder(w).Encode(struct {
		Path      string `json:"path"`
		Diff      string `json:"diff"`
		Truncated bool   `json:"truncated"`
	}{Path: path, Diff: text, Truncated: truncated})
}

// workspaceRepo is the git identity the controller's session opened with. A
// controller that carries none answers for a workspace git is not asked about.
func workspaceRepo(ctl control.SessionAPI) gitcmd.Repo {
	if c, ok := ctl.(interface{ WorkspaceRepo() gitcmd.Repo }); ok {
		return c.WorkspaceRepo()
	}
	return gitcmd.Repo{Dir: ctl.WorkspaceRoot()}
}
