package jobs

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	fileencoding "reasonix/internal/base/fileutil/encoding"
	"reasonix/internal/safety/evidence"
	"reasonix/internal/state/store"
)

// jobTimes is the job's timeline in epoch milliseconds. activity is what stall
// detection reads and is not derivable from the other two: a job can run for an
// hour while its output stopped moving in the first minute.
type jobTimes struct {
	started  int64
	activity int64
	finished int64
}

// touch records that the job's output just moved.
func (t *jobTimes) touch() { t.activity = nowMs() }

// jobArtifact is a job's durable output: the log file it streams into, the
// metadata beside it, and whether that pair ended up complete. One lifetime —
// opened when the job starts, closed when it ends — and one rule for the error.
// Not itself locked: every method runs under the owning Job's mu.
type jobArtifact struct {
	path     string
	metaPath string
	file     *os.File
	complete bool
	err      string
}

// noteErr keeps the first failure. A later one is usually its consequence —
// once the disk is full every write fails, and the last of those messages says
// nothing the first did not.
func (a *jobArtifact) noteErr(err error) {
	if err != nil && a.err == "" {
		a.err = err.Error()
	}
}

// open reports whether there is a file to write to.
func (a *jobArtifact) open() bool { return a.file != nil }

// write streams to the file when the job has one.
func (a *jobArtifact) write(p []byte) {
	if a.file == nil {
		return
	}
	_, err := a.file.Write(p)
	a.noteErr(err)
}

// writeString is write for a job's terminal result text.
func (a *jobArtifact) writeString(s string) {
	if a.file == nil {
		return
	}
	_, err := a.file.WriteString(s)
	a.noteErr(err)
}

// close ends the file and keeps whatever going wrong told us.
func (a *jobArtifact) close() {
	if a.file == nil {
		return
	}
	a.noteErr(a.file.Close())
	a.file = nil
}

const (
	jobLogExt                       = ".log"
	jobMetaExt                      = ".json"
	defaultTailBytes                = 64 * 1024
	mutationEvidenceVersion         = 1
	recoveredBackgroundTaskToolName = "background_task_recovery"
)

// The artifact itself proves the mutation; only this grade keeps
// ClassifyMutationRisk's no-paths branch at RiskHigh instead of low.
func recoveredTaskMutationReceipt(paths []string) evidence.Receipt {
	return evidence.Receipt{
		ToolName:         recoveredBackgroundTaskToolName,
		Success:          true,
		Write:            true,
		Mutation:         true,
		Paths:            paths,
		MutationEvidence: evidence.MutationProven,
	}
}

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
	ID                      string                    `json:"id"`
	Kind                    string                    `json:"kind"`
	Label                   string                    `json:"label,omitempty"`
	SessionID               string                    `json:"sessionId,omitempty"`
	OwnerID                 string                    `json:"ownerId,omitempty"`
	Status                  Status                    `json:"status"`
	StartedAt               int64                     `json:"startedAt"`
	FinishedAt              int64                     `json:"finishedAt,omitempty"`
	ArtifactComplete        bool                      `json:"artifactComplete"`
	ArtifactError           string                    `json:"artifactError,omitempty"`
	LogPath                 string                    `json:"logPath,omitempty"`
	MutationEvidenceVersion int                       `json:"mutationEvidenceVersion,omitempty"`
	MutationEvidence        *artifactMutationEvidence `json:"mutationEvidence,omitempty"`
}

// ArtifactView is the content-free projection used by machine-facing status
// surfaces. It deliberately excludes labels, outputs, paths, and mutation
// evidence because those fields may contain user or workspace data.
type ArtifactView struct {
	ID               string
	Kind             string
	Status           Status
	StartedAt        int64
	FinishedAt       int64
	ArtifactComplete bool
}

// ListArtifactViews returns persisted background-job metadata for one session.
// Missing artifact directories are normal and return an empty list.
func ListArtifactViews(sessionPath string) ([]ArtifactView, error) {
	dir := ArtifactDir(sessionPath)
	if strings.TrimSpace(dir) == "" {
		return nil, nil
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	out := make([]ArtifactView, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), jobMetaExt) {
			continue
		}
		meta, err := readMeta(filepath.Join(dir, entry.Name()))
		if err != nil {
			return nil, err
		}
		if strings.TrimSpace(meta.ID) == "" {
			continue
		}
		artifactComplete := persistedArtifactComplete(dir, meta)
		out = append(out, ArtifactView{
			ID:               meta.ID,
			Kind:             meta.Kind,
			Status:           meta.Status,
			StartedAt:        meta.StartedAt,
			FinishedAt:       meta.FinishedAt,
			ArtifactComplete: artifactComplete,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].StartedAt == out[j].StartedAt {
			return out[i].ID < out[j].ID
		}
		return out[i].StartedAt > out[j].StartedAt
	})
	return out, nil
}

func persistedArtifactComplete(dir string, meta artifactMeta) bool {
	switch meta.Status {
	case Done, Failed, Killed, Interrupted:
	default:
		return false
	}
	if !meta.ArtifactComplete || strings.TrimSpace(meta.ArtifactError) != "" {
		return false
	}
	logName := strings.TrimSpace(meta.LogPath)
	if logName == "" {
		logName = meta.ID + jobLogExt
	}
	info, err := os.Stat(filepath.Join(dir, filepath.Base(logName)))
	return err == nil && info.Mode().IsRegular()
}

// artifactMutationEvidence deliberately excludes receipt args, commands, and
// review contents. After a restart the parent must re-inspect and re-verify the
// recovered mutation rather than trusting stale child sign-off evidence.
type artifactMutationEvidence struct {
	Risk  string   `json:"risk"`
	Paths []string `json:"paths,omitempty"`
}

func writeMeta(path string, meta artifactMeta) error {
	if path == "" {
		return fmt.Errorf("empty metadata path")
	}
	if err := ensurePrivateArtifactDir(filepath.Dir(path)); err != nil {
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
