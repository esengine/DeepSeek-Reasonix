package serve

import (
	"context"
	"strings"

	"reasonix/internal/assembly/boot"
	"reasonix/internal/contract/config"
	"reasonix/internal/session/control"
)

// switchModelRequested is the user-facing model switch. A request that already
// names the running model returns before the busy guard and before any rebuild;
// internal callers that must refresh the same model keep using switchModel.
func (s *Server) switchModelRequested(ctx context.Context, ref string) error {
	s.bindMu.Lock()
	defer s.bindMu.Unlock()
	cur := s.ctl()
	if cfg, err := runtimeConfig(cur); err == nil {
		if err := cfg.RequireAnswers(ref, config.AnswersChat); err != nil {
			return err
		}
	}
	if target, ok := runtimeTargetForModel(cur, ref); ok && runtimeSelectionMatches(cur, target) {
		return nil
	}
	return s.switchModelLocked(ctx, ref)
}

func runtimeTargetForModel(cur control.SessionAPI, ref string) (control.RuntimeSelection, bool) {
	cfg, err := runtimeConfig(cur)
	if err != nil {
		return control.RuntimeSelection{}, false
	}
	entry, ok := cfg.ResolveModel(strings.TrimSpace(ref))
	if !ok {
		return control.RuntimeSelection{}, false
	}
	identity := boot.ResolveProviderBuildIdentity(entry, cfg.NetworkProxySpec(), nil)
	return control.RuntimeSelection{
		ModelRef:            entry.Name + "/" + entry.Model,
		Effort:              identity.Effort,
		ProviderFingerprint: identity.Fingerprint,
	}, identity.Fingerprint != ""
}

func runtimeConfig(cur control.SessionAPI) (*config.Config, error) {
	root := ""
	if cur != nil {
		root = strings.TrimSpace(cur.WorkspaceRoot())
	}
	return config.LoadForRootReadOnly(root)
}

func runtimeSelectionMatches(cur control.SessionAPI, target control.RuntimeSelection) bool {
	matcher, ok := cur.(control.RuntimeSelectionMatcher)
	return ok && matcher.MatchesRuntimeSelection(target)
}
