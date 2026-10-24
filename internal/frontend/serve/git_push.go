package serve

import (
	"encoding/json"
	"errors"
	"net/http"

	"reasonix/internal/platform/gitcmd"
	"reasonix/internal/platform/gitpush"
	"reasonix/internal/platform/gitrepos"
	"reasonix/internal/session/control"
)

// pushPreview says where the current branch would go and what it would send. It
// reaches no remote.
func (s *Server) pushPreview(w http.ResponseWriter, r *http.Request) {
	if refuseDevice(w, r) {
		return
	}
	plan, err := s.ctl().GitPushPreview(r.Context(), r.URL.Query().Get("repo"))
	if err != nil {
		refusePush(w, r, err)
		return
	}
	writeJSON(w, plan)
}

// push sends the confirmed plan's branch to its upstream.
func (s *Server) push(w http.ResponseWriter, r *http.Request) {
	if refuseDevice(w, r) {
		return
	}
	var req control.PushRequest
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&req); err != nil {
		refuse(w, http.StatusBadRequest, "push.bad_request", "the request body is not a JSON push request", nil)
		return
	}
	res, err := s.ctl().GitPush(r.Context(), req)
	if err != nil {
		refusePush(w, r, err)
		return
	}
	writeJSON(w, res)
}

// refusePush names why a push did not happen by what failed, never by reading
// what git said: a person told "needs authentication" does something different
// from one told the remote moved on.
func refusePush(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case r.Context().Err() != nil:
		// The person cancelled; nobody is left to read an answer.
	case refuseMutation(w, err):
	case errors.Is(err, control.ErrWorkspaceNotTrusted):
		refuse(w, http.StatusForbidden, "push.workspace_untrusted", "this workspace is not trusted, so nothing is pushed from it", nil)
	case errors.Is(err, gitrepos.ErrNotListed):
		refuse(w, http.StatusNotFound, "git.repo_unknown", "that repository is not part of this workspace", nil)
	case errors.Is(err, gitcmd.ErrNotRepository):
		refuse(w, http.StatusConflict, "commit.no_repository", err.Error(), nil)
	case errors.Is(err, gitpush.ErrDetachedHead):
		refuse(w, http.StatusConflict, "push.detached_head", "HEAD is not on a branch", nil)
	case errors.Is(err, gitpush.ErrNoUpstream):
		refuse(w, http.StatusConflict, "push.no_upstream", "this branch has no upstream to push to", nil)
	case errors.Is(err, gitpush.ErrNothingToPush):
		refuse(w, http.StatusConflict, "push.nothing_to_push", "the upstream already has every commit", nil)
	case errors.Is(err, gitpush.ErrProtocolNotAllowed):
		refuse(w, http.StatusForbidden, "push.protocol_not_allowed", "the remote's protocol is not one a push may use", nil)
	case errors.Is(err, gitpush.ErrPlanChanged):
		refuse(w, http.StatusConflict, "push.plan_changed", "the repository changed since the push was previewed", nil)
	case errors.Is(err, gitpush.ErrRejected):
		refuse(w, http.StatusConflict, "push.rejected", "the remote has commits this branch does not", nil)
	case errors.Is(err, gitpush.ErrAuthRequired):
		refuse(w, http.StatusConflict, "push.auth_required", "the remote needs authentication", nil)
	case errors.Is(err, gitpush.ErrPushFailed):
		refuse(w, http.StatusBadGateway, "push.failed", err.Error(), nil)
	default:
		refuse(w, http.StatusInternalServerError, "push.git_failed", err.Error(), nil)
	}
}
