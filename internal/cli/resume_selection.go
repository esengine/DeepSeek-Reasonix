package cli

import (
	"strings"

	"reasonix/internal/agent"
	"reasonix/internal/config"
	"reasonix/internal/control"
)

// modelForResumePath answers which model a resumed run should use. An explicit
// flag always wins; otherwise the saved selection is validated against the
// connection it was recorded against, so a migrated or edited connection fails
// closed instead of silently resuming under a different account.
func modelForResumePath(modelName, resumePath string, cfg *config.Config) (string, error) {
	if strings.TrimSpace(modelName) != "" || strings.TrimSpace(resumePath) == "" {
		return modelName, nil
	}
	sessionModel, identity, ok := agent.LoadSessionModelSelection(resumePath)
	if !ok {
		return modelName, nil
	}
	if cfg == nil {
		return sessionModel, nil
	}
	resolved, err := cfg.ResolveSavedModel(sessionModel, identity)
	if err != nil {
		return "", err
	}
	if _, ok := cfg.ResolveModel(resolved); !ok {
		return modelName, nil
	}
	return resolved, nil
}

func applyResumeModel(model *string, resumePath string, cfg *config.Config) error {
	resolved, err := modelForResumePath(*model, resumePath, cfg)
	if err != nil {
		return err
	}
	*model = resolved
	return nil
}

// copyResumableSession refuses to duplicate a session whose saved selection no
// longer resolves, so the copy cannot silently inherit an unusable connection.
func copyResumableSession(model, resumePath string, cfg *config.Config) (string, error) {
	if _, err := modelForResumePath(model, resumePath, cfg); err != nil {
		return "", err
	}
	return copySessionForWriting(resumePath)
}

// resumeWithPersistedSelection records the selection the resumed controller
// actually accepted, so the next restart restores it instead of re-resolving.
func resumeWithPersistedSelection(ctrl *control.Controller, session *agent.Session, path string) error {
	ctrl.Resume(session, path)
	return persistCLIModelSelection(ctrl)
}

// commitResumedSession is a no-op without a resume path, so callers need no
// second guard around the takeover handover.
func commitResumedSession(binding *cliTakeoverBinding, manager *cliTakeoverManager, ctrl *control.Controller, session *agent.Session, path string) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	if err := binding.commitPrevious(manager); err != nil {
		return err
	}
	return resumeWithPersistedSelection(ctrl, session, path)
}

// prepareServeSessionPath picks serve's auto-save target: reuse the resumed
// file, adopt the caller's session id, or leave the controller to stamp a
// fresh path.
func prepareServeSessionPath(ctrl *control.Controller, session *agent.Session, resumePath, sessionID string) error {
	if strings.TrimSpace(resumePath) != "" {
		return resumeWithPersistedSelection(ctrl, session, resumePath)
	}
	if strings.TrimSpace(sessionID) == "" {
		return nil
	}
	freshPath, err := freshWebSessionPath(ctrl.SessionDir(), sessionID)
	if err != nil {
		return err
	}
	ctrl.SetFreshSessionPath(freshPath)
	return nil
}
