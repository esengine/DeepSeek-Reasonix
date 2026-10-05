package boot

import (
	"strings"

	"reasonix/internal/contract/config"
	"reasonix/internal/state/sessionstore"
)

// ModelForResume names the model a resumed session should run on: an explicit
// choice wins, then the model recorded beside the transcript. A recorded model
// the catalog no longer resolves yields the caller's value so the default applies.
func ModelForResume(modelName, resumePath string, cfg *config.Config) string {
	if strings.TrimSpace(modelName) != "" || strings.TrimSpace(resumePath) == "" {
		return modelName
	}
	sessionModel, ok := sessionstore.LoadSessionModel(resumePath)
	if !ok {
		return modelName
	}
	if cfg == nil {
		return sessionModel
	}
	if _, ok := cfg.ResolveModel(sessionModel); !ok {
		return modelName
	}
	return sessionModel
}
