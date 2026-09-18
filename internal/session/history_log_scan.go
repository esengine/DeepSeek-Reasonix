package session

import (
	"context"
	"database/sql"
	"io"
	"os"
	"path/filepath"
	"strings"

	"reasonix/internal/sessioncontent"
)

// historyLogProgress couples coverage to the end of the same validated batch.
// Physical EOF may include an uncommitted tail and must never be a checkpoint.
type historyLogProgress struct {
	end       int64
	sequence  uint64
	modTimeNS int64
}

func (m historyIndexMetadata) canIncrement(sessionID string, revision logRevision, generation string, storageRevision int) bool {
	return m.sessionID == sessionID && m.storageRevision == storageRevision && m.projection == historyIndexVersion &&
		m.logSize >= 0 && m.logSize < revision.Size && strings.HasPrefix(m.generation, strings.TrimSuffix(generation, ":0")+":")
}

func configureHistoryRebuild(ctx context.Context, db *sql.DB) error {
	// Only applied to an unpublished, disposable replacement database.
	for _, pragma := range []string{
		`PRAGMA journal_mode=OFF`, `PRAGMA synchronous=OFF`, `PRAGMA locking_mode=EXCLUSIVE`,
		`PRAGMA cache_size=-8192`, `PRAGMA temp_store=FILE`,
	} {
		if _, err := db.ExecContext(ctx, pragma); err != nil {
			return err
		}
	}
	return nil
}

func scanHistoryLog(ctx context.Context, log *os.File, start int64, next uint64, limit int64, content *sessioncontent.Store, visit func(Commit) bool) (historyLogProgress, error) {
	progress := historyLogProgress{end: start, sequence: next - 1}
	info, err := log.Stat()
	if err != nil {
		return progress, err
	}
	progress.modTimeNS = info.ModTime().UnixNano()
	// A finite snapshot prevents continuous appends from extending the scan.
	reader := io.NewSectionReader(log, 0, min(limit, info.Size()))
	err = scanV4CommitFileRefs(ctx, reader, start, next, content, nil, func(_ int64, commit Commit) bool {
		if !visit(commit) {
			return false
		}
		progress.end, _ = reader.Seek(0, io.SeekCurrent) // SectionReader position cannot fail.
		progress.sequence = commit.LastSequence()
		return true
	})
	return progress, err
}

// Replacing a session while it is scanned must not publish an index for the
// old file under the replacement's identity. Appends preserve both identities.
func validateHistoryLog(ctx context.Context, dir string, log *os.File, generation string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	current, err := historyProjectionGeneration(dir, 0)
	if err != nil {
		return err
	}
	if current != generation {
		return ErrStaleGeneration
	}
	manifest, err := readStoredManifest(filepath.Join(dir, "manifest.json"))
	if err != nil {
		return err
	}
	currentFile, err := os.Stat(logPathForManifest(dir, manifest))
	if err != nil {
		return err
	}
	openedFile, err := log.Stat()
	if err != nil {
		return err
	}
	if !os.SameFile(currentFile, openedFile) {
		return ErrStaleGeneration
	}
	return nil
}
