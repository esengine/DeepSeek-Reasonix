package cli

import (
	"context"
	"fmt"
	"strings"

	"reasonix/internal/agent"
	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/session"
)

// modelForResumePath answers which model a resumed run should use. An explicit
// flag always wins; otherwise the saved selection is validated against the
// connection it was recorded against, so a migrated or edited connection fails
// closed instead of silently resuming under a different account.
func modelForResumePath(modelName, resumePath string, cfg *config.Config) (string, error) {
	if strings.TrimSpace(modelName) != "" || strings.TrimSpace(resumePath) == "" {
		return modelName, nil
	}
	id, native := v4ResumeID(resumePath)
	if !native {
		return modelName, nil
	}
	sessionModel, identity, ok := nativeResumeModelSelection(resolveCLISessionDir(), id)
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
	id, native := v4ResumeID(resumePath)
	if !native {
		return "", fmt.Errorf("unsupported resume target %q", resumePath)
	}
	return copyNativeResumeSession(model, id, cfg)
}

// copyNativeResumeSession duplicates a native v4 session under a fresh
// identity through the session service, returning its resume locator.
func copyNativeResumeSession(model, id string, cfg *config.Config) (string, error) {
	if _, err := modelForResumePath(model, v4ResumeLocator(id), cfg); err != nil {
		return "", err
	}
	service := nativeResumeService(resolveCLISessionDir())
	if service == nil {
		return "", fmt.Errorf("session service is unavailable")
	}
	result, err := service.CopySession(context.Background(), session.CopyRequest{
		Source:      session.SessionRef{HostID: service.HostID(), SessionID: id},
		OperationID: "cli-copy:" + agent.NewMessageID(),
	})
	if err != nil {
		return "", err
	}
	return v4ResumeLocator(result.Child.SessionID), nil
}

// resumeWithPersistedSelection attaches a native v4 resume target to the
// controller. The session service owns the writer lease, so no legacy import or
// single-writer lease is involved.
func resumeWithPersistedSelection(ctrl *control.Controller, path string) error {
	id, native := v4ResumeID(path)
	if !native {
		return fmt.Errorf("unsupported resume target %q", path)
	}
	service := ctrl.SessionService()
	if service == nil {
		return fmt.Errorf("session service is unavailable")
	}
	ref := session.SessionRef{HostID: service.HostID(), SessionID: id}
	_, err := ctrl.OpenSession(context.Background(), ref)
	return err
}

// commitResumedSession is a no-op without a resume path, so callers need no
// second guard around the takeover handover.
func commitResumedSession(binding *cliTakeoverBinding, manager *cliTakeoverManager, ctrl *control.Controller, path string) error {
	if strings.TrimSpace(path) == "" {
		return nil
	}
	if err := binding.commitPrevious(manager); err != nil {
		return err
	}
	return resumeWithPersistedSelection(ctrl, path)
}

// prepareServeSessionPath picks serve's auto-save target: reuse the resumed
// file, adopt the caller's session id, or leave the controller to stamp a
// fresh path.
func prepareServeSessionPath(ctrl *control.Controller, resumePath, sessionID string) error {
	if strings.TrimSpace(resumePath) != "" {
		return resumeWithPersistedSelection(ctrl, resumePath)
	}
	if strings.TrimSpace(sessionID) == "" {
		return nil
	}
	if ctrl.UsesExclusiveSession() {
		_, err := ctrl.BindFreshSession(context.Background(), strings.TrimSpace(sessionID))
		return err
	}
	freshPath, err := freshWebSessionPath(ctrl.SessionDir(), sessionID)
	if err != nil {
		return err
	}
	ctrl.SetFreshSessionPath(freshPath)
	return nil
}
