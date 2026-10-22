package jobs

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"reasonix/internal/base/fileutil"
	"reasonix/internal/contract/tool"
)

type toolResultArtifact struct {
	Version int             `json:"version"`
	Result  json.RawMessage `json:"result"`
}

func (m *Manager) hasUnscopedResultsLocked() bool {
	return m.scratch != nil && m.artifactDirs[""] == filepath.Join(m.scratch.Path(), "default")
}

// SaveToolResultForSession retains all result fields without changing session formats.
func (m *Manager) SaveToolResultForSession(session, callID string, raw json.RawMessage) (string, error) {
	if err := validatePathSegment(session, "parentSession"); err != nil {
		return "", err
	}
	if !json.Valid(raw) {
		return "", fmt.Errorf("tool result is not valid JSON")
	}
	m.mu.Lock()
	dir := m.artifactDirLocked(session)
	m.artifactDirs[session] = dir
	m.mu.Unlock()
	sum := sha256.New()
	_, _ = fmt.Fprintf(sum, "%d:%s", len(callID), callID)
	_, _ = sum.Write(raw)
	digest := hex.EncodeToString(sum.Sum(nil))
	path := filepath.Join(dir, "result-"+digest+".tool-result")
	err := createSidecar(path, toolResultArtifact{Version: 1, Result: raw})
	return tool.ResultReferencePrefix + digest, err
}

func resultFilename(ref string) (string, error) {
	digest, ok := strings.CutPrefix(ref, tool.ResultReferencePrefix)
	b, err := hex.DecodeString(digest)
	if !ok || err != nil || len(b) != sha256.Size {
		return "", fmt.Errorf("invalid tool result reference")
	}
	return "result-" + strings.ToLower(digest) + ".tool-result", nil
}

// OpenToolResultForSession resolves only artifacts owned by the addressed session.
func (m *Manager) OpenToolResultForSession(session, ref string) (io.ReadCloser, error) {
	if err := validatePathSegment(session, "parentSession"); err != nil {
		return nil, err
	}
	name, err := resultFilename(ref)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	dir := m.artifactDirLocked(session)
	m.mu.Unlock()
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	f, err := root.Open(name)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		_ = f.Close()
		return nil, fmt.Errorf("tool result is not a readable artifact")
	}
	return f, nil
}

func writeSidecar(path string, value any) error {
	if err := ensurePrivateArtifactDir(filepath.Dir(path)); err != nil {
		return err
	}
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return fileutil.AtomicWriteFileStrict(path, b, 0o600)
}

func createSidecar(path string, value any) error {
	if err := ensurePrivateArtifactDir(filepath.Dir(path)); err != nil {
		return err
	}
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}
	err = fileutil.AtomicCreateFile(path, b, 0o600)
	if errors.Is(err, os.ErrExist) {
		stored, readErr := os.ReadFile(path)
		if readErr == nil && bytes.Equal(stored, b) {
			return nil
		}
	}
	return err
}
