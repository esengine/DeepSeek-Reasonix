package serve

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"reasonix/internal/platform/gitcommit"
	"reasonix/internal/runtime/commitmsg"
	"reasonix/internal/session/control"
)

func (s *Server) registerGitRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /changes", s.changes)
	mux.HandleFunc("GET /changes/diff", s.changeDiff)
	mux.HandleFunc("GET /workspace/git", s.workspaceGit)
	mux.HandleFunc("GET /workspace/branches", s.workspaceBranches)
	mux.HandleFunc("POST /workspace/branch/switch", s.workspaceSwitchBranch)
	mux.HandleFunc("POST /commit/propose", s.commitPropose)
	mux.HandleFunc("POST /commit", s.commitStaged)
}

// commitPropose answers the staged changes and a model-proposed message. It
// changes nothing in the repository.
func (s *Server) commitPropose(w http.ResponseWriter, r *http.Request) {
	p, err := s.ctl().ProposeCommit(r.Context())
	if err != nil {
		refuseCommit(w, r, err)
		return
	}
	writeJSON(w, p)
}

// commitStaged records a local commit of the staged set the person confirmed.
func (s *Server) commitStaged(w http.ResponseWriter, r *http.Request) {
	var req control.CommitRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 2*gitcommit.MaxMessageBytes)).Decode(&req); err != nil {
		refuse(w, http.StatusBadRequest, "commit.bad_request", "the request body is not a JSON commit", nil)
		return
	}
	res, err := s.ctl().CommitStaged(r.Context(), req)
	if err != nil {
		refuseCommit(w, r, err)
		return
	}
	writeJSON(w, res)
}

func refuseCommit(w http.ResponseWriter, r *http.Request, err error) {
	var sensitive *gitcommit.SensitiveError
	switch {
	case r.Context().Err() != nil:
		// The person cancelled; nobody is left to read an answer.
	case errors.Is(err, gitcommit.ErrNoRepository):
		refuse(w, http.StatusConflict, "commit.no_repository", err.Error(), nil)
	case errors.Is(err, gitcommit.ErrNothingStaged):
		refuse(w, http.StatusConflict, "commit.nothing_staged", err.Error(), nil)
	case errors.Is(err, gitcommit.ErrStagedChanged):
		refuse(w, http.StatusConflict, "commit.staged_changed", err.Error(), nil)
	case errors.As(err, &sensitive):
		refuse(w, http.StatusConflict, "commit.secrets_staged", err.Error(),
			map[string]any{"files": sensitive.Files, "content": sensitive.Content})
	case errors.Is(err, gitcommit.ErrEmptyMessage):
		refuse(w, http.StatusBadRequest, "commit.empty_message", err.Error(), nil)
	case errors.Is(err, gitcommit.ErrMessageInvalid):
		refuse(w, http.StatusBadRequest, "commit.message_invalid", err.Error(), nil)
	case errors.Is(err, gitcommit.ErrMessageTooLong):
		refuse(w, http.StatusRequestEntityTooLarge, "commit.message_too_long", err.Error(),
			map[string]any{"max_bytes": gitcommit.MaxMessageBytes})
	case errors.Is(err, gitcommit.ErrIdentityMissing):
		refuse(w, http.StatusConflict, "commit.identity_missing", err.Error(), nil)
	case errors.Is(err, gitcommit.ErrCommitFailed):
		refuse(w, http.StatusInternalServerError, "commit.failed", err.Error(), nil)
	case errors.Is(err, commitmsg.ErrUnavailable):
		refuse(w, http.StatusConflict, "commit.no_model", err.Error(), nil)
	case errors.Is(err, context.DeadlineExceeded):
		refuse(w, http.StatusGatewayTimeout, "commit.timeout", err.Error(), nil)
	case errors.Is(err, commitmsg.ErrNoAnswer):
		refuse(w, http.StatusBadGateway, "commit.no_answer", err.Error(), nil)
	default:
		refuse(w, http.StatusInternalServerError, "commit.git_failed", err.Error(), nil)
	}
}
