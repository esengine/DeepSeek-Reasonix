package jobs

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	fileencoding "reasonix/internal/fileutil/encoding"
	"reasonix/internal/store"
)

const (
	jobLogExt        = ".log"
	jobMetaExt       = ".json"
	defaultTailBytes = 64 * 1024
)

// ArtifactDir returns the sidecar directory for a persistent session transcript.
func ArtifactDir(sessionPath string) string {
	return store.SessionJobsDir(sessionPath)
}

// RemoveArtifacts removes the job sidecar for a session transcript.
func RemoveArtifacts(sessionPath string) error {
	dir := ArtifactDir(sessionPath)
	if dir == "" {
		return nil
	}
	return os.RemoveAll(dir)
}

type artifactMeta struct {
	ID               string `json:"id"`
	Kind             string `json:"kind"`
	Label            string `json:"label,omitempty"`
	SessionID        string `json:"sessionId,omitempty"`
	Status           Status `json:"status"`
	StartedAt        int64  `json:"startedAt"`
	FinishedAt       int64  `json:"finishedAt,omitempty"`
	ArtifactComplete bool   `json:"artifactComplete"`
	ArtifactError    string `json:"artifactError,omitempty"`
	LogPath          string `json:"logPath,omitempty"`
}

func writeMeta(path string, meta artifactMeta) error {
	if path == "" {
		return fmt.Errorf("empty metadata path")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".job-meta-*.tmp")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		os.Remove(tmpPath)
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return os.Rename(tmpPath, path)
}

func readMeta(path string) (artifactMeta, error) {
	var meta artifactMeta
	b, err := fileencoding.ReadFileUTF8(path)
	if err != nil {
		return meta, err
	}
	if err := json.Unmarshal(b, &meta); err != nil {
		return meta, err
	}
	return meta, nil
}

func maxJobSeq(id string) int {
	_, tail, ok := strings.Cut(id, "-")
	if !ok {
		return 0
	}
	n, err := strconv.Atoi(tail)
	if err != nil {
		return 0
	}
	return n
}

func appendTail(buf []byte, p []byte, limit int) []byte {
	if limit <= 0 {
		return nil
	}
	if len(p) >= limit {
		out := make([]byte, limit)
		copy(out, p[len(p)-limit:])
		return out
	}
	total := len(buf) + len(p)
	if total <= limit {
		out := make([]byte, total)
		copy(out, buf)
		copy(out[len(buf):], p)
		return out
	}
	keep := limit - len(p)
	out := make([]byte, limit)
	copy(out, buf[len(buf)-keep:])
	copy(out[keep:], p)
	return out
}
