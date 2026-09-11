package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Uploads use a private copy of an owned file. OpenRoot keeps a concurrent
// symlink replacement from escaping the authorized workspace/scratch root.
// Remote broker uploads are first downloaded into this executor's scratch
// root, so they follow the same policy without trusting remote absolute paths.
func (e *hostBrowserExecutor) prepareUploadFiles(files []string) ([]string, func(), error) {
	if len(files) == 0 {
		return nil, nil, fmt.Errorf("browser upload: files are required")
	}
	scratch, err := e.captureDir()
	if err != nil {
		return nil, nil, err
	}
	roots := []string{scratch}
	e.app.mu.RLock()
	if tab := e.app.tabs[e.tabID]; tab != nil && tab.WorkspaceRoot != "" {
		roots = append(roots, tab.WorkspaceRoot)
	}
	e.app.mu.RUnlock()
	dir, err := os.MkdirTemp(scratch, "upload-")
	if err != nil {
		return nil, nil, err
	}
	cleanup := func() { _ = os.RemoveAll(dir) }
	result := make([]string, 0, len(files))
	for _, file := range files {
		staged, err := stageOwnedBrowserFile(roots, dir, file)
		if err != nil {
			cleanup()
			return nil, nil, err
		}
		result = append(result, staged)
	}
	return result, cleanup, nil
}

func stageOwnedBrowserFile(roots []string, directory, file string) (string, error) {
	if !filepath.IsAbs(file) {
		return "", fmt.Errorf("browser upload: file path must be absolute")
	}
	name := filepath.Base(file)
	if !filepath.IsLocal(name) || name == "." || strings.ContainsAny(name, "/\\\x00") {
		return "", fmt.Errorf("browser upload: file name must be a single local path component")
	}
	resolved, err := filepath.EvalSymlinks(file)
	if err != nil {
		return "", fmt.Errorf("browser upload: %w", err)
	}
	for _, candidate := range roots {
		rootPath, err := filepath.EvalSymlinks(candidate)
		if err != nil {
			continue
		}
		rel, err := filepath.Rel(rootPath, resolved)
		if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
			continue
		}
		root, err := os.OpenRoot(rootPath)
		if err != nil {
			return "", err
		}
		input, err := root.Open(rel)
		_ = root.Close()
		if err != nil {
			return "", err
		}
		defer input.Close()
		info, err := input.Stat()
		if err != nil {
			return "", err
		}
		if !info.Mode().IsRegular() || info.Size() > browserRelayMaxBytes {
			return "", fmt.Errorf("browser upload: file must be regular and at most %d bytes", browserRelayMaxBytes)
		}
		dir, err := os.MkdirTemp(directory, "file-")
		if err != nil {
			return "", err
		}
		outputRoot, err := os.OpenRoot(dir)
		if err != nil {
			return "", err
		}
		defer outputRoot.Close()
		out, err := outputRoot.OpenFile(name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			return "", err
		}
		n, copyErr := io.Copy(out, io.LimitReader(input, browserRelayMaxBytes+1))
		closeErr := out.Close()
		if n > browserRelayMaxBytes {
			copyErr = errors.Join(copyErr, fmt.Errorf("file exceeds %d bytes", browserRelayMaxBytes))
		}
		if copyErr != nil || closeErr != nil {
			_ = outputRoot.Remove(name)
			return "", fmt.Errorf("browser upload: staging failed: %w", errors.Join(copyErr, closeErr))
		}
		return filepath.Join(dir, name), nil
	}
	return "", fmt.Errorf("browser upload: file is outside this task's workspace and scratch directory")
}
