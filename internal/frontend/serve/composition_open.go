package serve

import (
	"errors"
	"net/http"
	"path/filepath"

	"reasonix/internal/contract/agentcomposition"
	"reasonix/internal/state/sessionstore"
	"reasonix/internal/state/store"
)

func loadOpenComposition(root, path string) (*agentcomposition.Binding, error) {
	if !store.IsSessionTranscriptName(filepath.Base(path)) {
		return nil, refusal(http.StatusBadRequest, codeSessionBadPath, errors.New("invalid session path"), nil)
	}
	realPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		status, refused := pathUnresolved(err)
		return nil, keepRefusalStatus(status, refused)
	}
	realDir, err := filepath.EvalSymlinks(SessionDirFor(root))
	if err != nil {
		status, refused := pathUnresolved(err)
		return nil, keepRefusalStatus(status, refused)
	}
	rel, err := filepath.Rel(realDir, realPath)
	if err != nil || !filepath.IsLocal(rel) {
		return nil, refusal(http.StatusForbidden, codeSessionOutside, errors.New("path outside session dir"), nil)
	}
	if err := sessionstore.PrepareSessionPath(realPath); err != nil {
		return nil, err
	}
	loaded, err := sessionstore.LoadSession(realPath)
	if err != nil {
		return nil, err
	}
	return loaded.Composition(), nil
}
