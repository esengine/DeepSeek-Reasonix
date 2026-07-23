// Package mirror stores local read-only copies of remote session checkpoints.
package mirror

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"reasonix/internal/config"
)

// Root is <Reasonix home>/remote-mirrors.
func Root() string {
	base := config.MemoryUserDir()
	if base == "" {
		return ""
	}
	return filepath.Join(base, "remote-mirrors")
}

// Store is a fingerprint+workspace scoped mirror tree.
type Store struct {
	Base string
}

func (s Store) root() string {
	if strings.TrimSpace(s.Base) != "" {
		return s.Base
	}
	return Root()
}

// Manifest describes one checkpoint.
type Manifest struct {
	SessionID   string            `json:"sessionId"`
	Revision    int64             `json:"revision"`
	Digest      string            `json:"digest"`
	ModelRef    string            `json:"modelRef,omitempty"`
	Label       string            `json:"label,omitempty"`
	CreatedAt   string            `json:"createdAt,omitempty"`
	ArtifactSHA map[string]string `json:"artifactSha,omitempty"`
}

// DigestArtifacts computes a stable path+content digest.
func DigestArtifacts(artifacts map[string][]byte) string {
	if len(artifacts) == 0 {
		return ""
	}
	paths := make([]string, 0, len(artifacts))
	for p := range artifacts {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	h := sha256.New()
	for _, p := range paths {
		_, _ = h.Write([]byte(p))
		_, _ = h.Write([]byte{0})
		sum := sha256.Sum256(artifacts[p])
		_, _ = h.Write(sum[:])
	}
	return hex.EncodeToString(h.Sum(nil))
}

// SessionDir returns the mirror directory for a session.
func (s Store) SessionDir(fingerprint, workspace, sessionID string) string {
	root := s.root()
	if root == "" {
		return ""
	}
	fp := shortHash(fingerprint)
	ws := shortHash(workspace)
	safe := config.BoundFilenameComponent(sessionID, 200)
	return filepath.Join(root, fp, ws, "sessions", safe)
}

// ApplyCheckpoint validates digest/SHA then atomically writes artifacts.
// Returns error without partial writes on mismatch or missing session.jsonl when required.
func (s Store) ApplyCheckpoint(fingerprint, workspace string, man Manifest, artifacts map[string][]byte) error {
	if strings.TrimSpace(man.SessionID) == "" {
		return fmt.Errorf("checkpoint missing sessionId")
	}
	got := DigestArtifacts(artifacts)
	if man.Digest != "" && !strings.EqualFold(man.Digest, got) {
		return fmt.Errorf("checkpoint digest mismatch")
	}
	if man.Digest == "" {
		man.Digest = got
	}
	for path, want := range man.ArtifactSHA {
		data, ok := artifacts[path]
		if !ok {
			return fmt.Errorf("missing artifact %q", path)
		}
		sum := sha256.Sum256(data)
		if !strings.EqualFold(want, hex.EncodeToString(sum[:])) && !strings.EqualFold(want, man.Digest) {
			// Allow digest of whole set stored under session.jsonl key for v1.
			if path != "session.jsonl" || !strings.EqualFold(want, man.Digest) {
				return fmt.Errorf("artifact sha mismatch for %q", path)
			}
		}
	}
	if _, ok := artifacts["session.jsonl"]; !ok {
		return fmt.Errorf("checkpoint missing session.jsonl")
	}

	dir := s.SessionDir(fingerprint, workspace, man.SessionID)
	if dir == "" {
		return fmt.Errorf("mirror path unavailable")
	}
	if err := os.MkdirAll(filepath.Dir(dir), 0o700); err != nil {
		return err
	}
	tmp, err := os.MkdirTemp(filepath.Dir(dir), ".mirror-"+man.SessionID+"-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)

	man.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	manBytes, err := json.MarshalIndent(man, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(tmp, "checkpoint.json"), manBytes, 0o600); err != nil {
		return err
	}
	for rel, data := range artifacts {
		rel = filepath.Clean(rel)
		if rel == "." || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
			return fmt.Errorf("invalid artifact path %q", rel)
		}
		path := filepath.Join(tmp, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return err
		}
		if err := os.WriteFile(path, data, 0o600); err != nil {
			return err
		}
	}
	_ = os.RemoveAll(dir)
	if err := os.Rename(tmp, dir); err != nil {
		return err
	}
	return nil
}

// ReadSessionJSONL loads mirrored session body for restore-as-new-session.
func (s Store) ReadSessionJSONL(fingerprint, workspace, sessionID string) ([]byte, Manifest, error) {
	var man Manifest
	dir := s.SessionDir(fingerprint, workspace, sessionID)
	if dir == "" {
		return nil, man, fmt.Errorf("mirror path unavailable")
	}
	manBytes, err := os.ReadFile(filepath.Join(dir, "checkpoint.json"))
	if err != nil {
		return nil, man, err
	}
	if err := json.Unmarshal(manBytes, &man); err != nil {
		return nil, man, err
	}
	data, err := os.ReadFile(filepath.Join(dir, "session.jsonl"))
	if err != nil {
		return nil, man, err
	}
	// Re-validate digest.
	arts := map[string][]byte{"session.jsonl": data}
	if man.Digest != "" && !strings.EqualFold(man.Digest, DigestArtifacts(arts)) {
		return nil, man, fmt.Errorf("stored mirror digest mismatch")
	}
	return data, man, nil
}

func shortHash(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:8])
}
