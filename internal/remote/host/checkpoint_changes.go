package host

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"reasonix/internal/control"
)

// CheckpointChange is the neutral read-only file-change projection used by
// Host file/Git queries. It deliberately contains neither file contents nor a
// Controller-private checkpoint identity.
type CheckpointChange struct {
	Path       string
	Turn       int
	Prompt     string
	TimeMillis int64
}

// CheckpointChanges freezes Controller checkpoint metadata at a Session actor
// barrier and flattens every changed path into an owned value. Callers never
// reach around SessionRuntime to race the Controller during replacement.
func (r *SessionRuntime) CheckpointChanges(ctx context.Context) ([]CheckpointChange, error) {
	value, err := r.call(ctx, func(*runtimeActorState) (any, error) {
		var snapshot control.CheckpointSnapshot
		if err := safeControllerCall(func() { snapshot = r.controller.CheckpointSnapshot() }); err != nil {
			return nil, err
		}
		count := 0
		for _, meta := range snapshot.Metas {
			if meta.Turn < 0 {
				return nil, fmt.Errorf("checkpoint turn %d is negative", meta.Turn)
			}
			count += len(meta.Paths)
		}
		changes := make([]CheckpointChange, 0, count)
		for _, meta := range snapshot.Metas {
			at := int64(0)
			if !meta.Time.IsZero() {
				at = meta.Time.UnixMilli()
			}
			for _, path := range meta.Paths {
				relative, err := primaryRelativeCheckpointPath(r.workspaceRoot, path)
				if err != nil {
					return nil, err
				}
				changes = append(changes, CheckpointChange{
					Path: relative, Turn: meta.Turn, Prompt: meta.Prompt, TimeMillis: at,
				})
			}
		}
		return changes, nil
	})
	if err != nil {
		return nil, err
	}
	return value.([]CheckpointChange), nil
}

func primaryRelativeCheckpointPath(workspaceRoot, path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", errors.New("checkpoint path is empty")
	}
	cleaned := filepath.Clean(path)
	if filepath.IsAbs(cleaned) {
		if strings.TrimSpace(workspaceRoot) == "" {
			return "", errors.New("absolute checkpoint path has no workspace root")
		}
		relative, err := filepath.Rel(filepath.Clean(workspaceRoot), cleaned)
		if err != nil {
			return "", fmt.Errorf("make checkpoint path workspace-relative: %w", err)
		}
		cleaned = filepath.Clean(relative)
	}
	if cleaned == "." || cleaned == ".." || filepath.IsAbs(cleaned) || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("checkpoint path escapes primary workspace: %q", path)
	}
	return filepath.ToSlash(cleaned), nil
}
