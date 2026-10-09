package builtin

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"reasonix/internal/base/fileutil"
	"reasonix/internal/state/sessiontemp"
)

// canonicalWriterPath is the lease and grant identity of a write target. The
// writers themselves keep the path as the caller spelled it, so receipts, file
// views and refusals name what the model used.
func canonicalWriterPath(workDir string, temp *sessiontemp.Manager, roots []string, path string) (string, error) {
	path = resolveIn(workDir, resolveSessionTemp(temp, path))
	if err := refuseNetworkPath(path, roots); err != nil {
		return path, err
	}
	canonical, err := fileutil.CanonicalWritePath(path)
	if err != nil {
		return path, err
	}
	return canonical, nil
}

// ResolveWritePaths returns grant targets even when their narrow lease identity
// is ambiguous. Lease callers must retain whole-workspace exclusion on error.
func ResolveWritePaths(workDir string, temp *sessiontemp.Manager, roots []string, args json.RawMessage, move bool) ([]string, error) {
	var p struct {
		Path        string `json:"path"`
		Source      string `json:"source_path"`
		Destination string `json:"destination_path"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return nil, fmt.Errorf("invalid args: %w", err)
	}
	paths := []string{p.Path}
	if move {
		paths = []string{p.Source, p.Destination}
	}
	if slices.Contains(paths, "") {
		return nil, fmt.Errorf("write path is required")
	}
	var identityErr error
	for i, path := range paths {
		resolved, err := canonicalWriterPath(workDir, temp, roots, path)
		if err != nil {
			if !errors.Is(err, fileutil.ErrAmbiguousPath) {
				return nil, err
			}
			identityErr = fileutil.ErrAmbiguousPath
			resolved, err = realPath(resolved)
			if err != nil {
				return nil, err
			}
		}
		paths[i] = resolved
	}
	return paths, identityErr
}

func (w writeFile) WritePaths(args json.RawMessage) ([]string, error) {
	return ResolveWritePaths(w.workDir, w.sessionTemp, w.roots, args, false)
}
func (e editFile) WritePaths(args json.RawMessage) ([]string, error) {
	return ResolveWritePaths(e.workDir, e.sessionTemp, e.roots, args, false)
}
func (m multiEdit) WritePaths(args json.RawMessage) ([]string, error) {
	return ResolveWritePaths(m.workDir, m.sessionTemp, m.roots, args, false)
}
func (n notebookEdit) WritePaths(args json.RawMessage) ([]string, error) {
	return ResolveWritePaths(n.workDir, n.sessionTemp, n.roots, args, false)
}
func (d deleteRange) WritePaths(args json.RawMessage) ([]string, error) {
	return ResolveWritePaths(d.workDir, d.sessionTemp, d.roots, args, false)
}
func (d deleteSymbol) WritePaths(args json.RawMessage) ([]string, error) {
	return ResolveWritePaths(d.workDir, d.sessionTemp, d.roots, args, false)
}
func (m moveFile) WritePaths(args json.RawMessage) ([]string, error) {
	return ResolveWritePaths(m.workDir, m.sessionTemp, m.roots, args, true)
}
