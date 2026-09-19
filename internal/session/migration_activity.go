package session

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/filelock"
	"reasonix/internal/sessioncontent"
)

// sourceActivityTimes resolves a migrated target's created and updated times
// from the preserved source. An unrecorded source falls back to the import time
// so the target still has a usable timeline.
func sourceActivityTimes(source Source) (created, updated time.Time) {
	created, updated = source.CreatedAt, source.UpdatedAt
	if created.IsZero() {
		created = updated
	}
	if created.IsZero() {
		created = time.Now().UTC()
	}
	if updated.IsZero() {
		updated = created
	}
	return created, updated
}

// prototypeSourceTimes records a prototype/v3 source's creation and activity
// times on the target source, falling back to the import time when the source
// carries neither.
func prototypeSourceTimes(source *Source, prototype Manifest, lastActivity time.Time) (created, updated time.Time) {
	if source.CreatedAt.IsZero() {
		source.CreatedAt = prototype.CreatedAt
	}
	if source.UpdatedAt.IsZero() {
		source.UpdatedAt = lastActivity
	}
	created, updated = sourceActivityTimes(*source)
	source.CreatedAt, source.UpdatedAt = created, updated
	return created, updated
}

// stampLogActivity sets the durable log's mtime to the session's real last
// activity so listing and resume order by when the conversation happened.
func stampLogActivity(logPath string, updatedAt time.Time) error {
	if updatedAt.IsZero() {
		return nil
	}
	if err := os.Chtimes(logPath, updatedAt, updatedAt); err != nil {
		return fmt.Errorf("stamp migrated session activity: %w", err)
	}
	return nil
}

// validateImportedPreview replays a freshly published target and confirms it
// matches the import's commit count and committed sequence.
func validateImportedPreview(ctx context.Context, logPath string, content *sessioncontent.Store, wantCommits int, wantSequence uint64) error {
	file, err := os.Open(logPath)
	if err != nil {
		return err
	}
	projection := Projection{}
	commits := 0
	var validationErr error
	replayErr := scanV4CommitFile(ctx, file, 0, 1, content, nil, func(_ int64, commit Commit) bool {
		if err := applyProjectionCommit(&projection, commit); err != nil {
			validationErr = err
			return false
		}
		commits++
		return true
	})
	replayErr = errors.Join(replayErr, validationErr, file.Close())
	if replayErr == nil && commits == wantCommits && projection.CommittedSequence == wantSequence {
		return nil
	}
	if replayErr == nil {
		replayErr = fmt.Errorf("replayed %d commits through sequence %d; want %d through %d", commits, projection.CommittedSequence, wantCommits, wantSequence)
	}
	return fmt.Errorf("validate prototype target: %w", replayErr)
}

// RepairMigratedActivity backfills the source created/updated times onto
// migrated sessions published before those times were recorded. Without it,
// resume orders such a session by its import time, which pushes every pre-v4
// conversation to the top of the list. The preserved legacy/ evidence supplies
// the times, so a target whose evidence is missing or has been continued is
// left untouched, and the recorded Source times make later passes no-ops.
func RepairMigratedActivity(ctx context.Context, root string) (int, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return 0, nil
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	repaired := 0
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return repaired, err
		}
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		done, repairErr := repairMigratedSessionActivity(ctx, filepath.Join(root, entry.Name()))
		if repairErr != nil {
			return repaired, repairErr
		}
		if done {
			repaired++
		}
	}
	return repaired, nil
}

// repairMigratedSessionActivity stamps one migrated target's recorded times. It
// returns false for a native session, an already-recorded target, one whose
// evidence is gone, one that has been continued, or one a live writer owns.
func repairMigratedSessionActivity(ctx context.Context, dir string) (bool, error) {
	manifestPath := filepath.Join(dir, "manifest.json")
	manifest, err := readManifest(manifestPath)
	if err != nil {
		// An unsupported, damaged, or absent target is not ours to repair.
		return false, nil
	}
	if manifest.Source == nil || !manifest.Source.UpdatedAt.IsZero() {
		return false, nil
	}
	created, updated, ok := migratedEvidenceActivity(dir, *manifest.Source)
	if !ok {
		return false, nil
	}
	// Never rewrite a session that has been continued: its log already carries
	// the real last-activity time, and the import prefix proves it is pristine.
	pristine, lastImport, err := importOnlyLog(ctx, dir)
	if err != nil || !pristine {
		return false, nil
	}
	if updated.IsZero() {
		updated = lastImport
	}
	if created.IsZero() {
		created = updated
	}
	if created.IsZero() {
		return false, nil
	}
	release, err := filelock.TryAcquire(filepath.Join(dir, "writer.lock"))
	if err != nil {
		// A live writer owns the target; a later pass repairs it.
		return false, nil
	}
	defer release()
	// Re-read under the writer lock in case a writer changed the manifest.
	manifest, err = readManifest(manifestPath)
	if err != nil || manifest.Source == nil || !manifest.Source.UpdatedAt.IsZero() {
		return false, nil
	}
	manifest.CreatedAt = created
	manifest.Source.CreatedAt = created
	manifest.Source.UpdatedAt = updated
	if err := writeManifest(manifestPath, manifest); err != nil {
		return false, err
	}
	if err := os.Chtimes(logPathForManifest(dir, manifest), updated, updated); err != nil {
		return false, err
	}
	return true, nil
}

// migratedEvidenceActivity reads the source's own times from the legacy/
// evidence directory. A JSONL source records them in its branch-metadata
// sidecar; a prototype/v3 source records the creation time in the frozen
// prototype manifest and the activity time in its converted commits (returned
// separately by importOnlyLog).
func migratedEvidenceActivity(dir string, source Source) (created, updated time.Time, ok bool) {
	evidence := filepath.Join(dir, "legacy")
	if source.Version == "legacy" {
		meta, found, err := agent.LoadBranchMeta(filepath.Join(evidence, filepath.Base(source.Path)))
		if err != nil || !found {
			return time.Time{}, time.Time{}, false
		}
		return meta.CreatedAt, meta.UpdatedAt, true
	}
	b, err := os.ReadFile(filepath.Join(evidence, "prototype", "manifest.json"))
	if err != nil {
		return time.Time{}, time.Time{}, false
	}
	var prototype Manifest
	if json.Unmarshal(b, &prototype) != nil {
		return time.Time{}, time.Time{}, false
	}
	return prototype.CreatedAt, time.Time{}, true
}

// importOnlyLog reports whether every commit in the target belongs to the
// migration import, and returns the newest commit time. A single non-import
// commit means the session has been continued.
func importOnlyLog(ctx context.Context, dir string) (bool, time.Time, error) {
	pristine := true
	var last time.Time
	err := VisitCommits(ctx, dir, func(commit Commit) error {
		if !isImportOperation(commit.OperationID) {
			pristine = false
		}
		if commit.CreatedAt.After(last) {
			last = commit.CreatedAt
		}
		return nil
	})
	return pristine, last, err
}

func isImportOperation(operationID string) bool {
	return strings.HasPrefix(operationID, "legacy-import:") || strings.HasPrefix(operationID, "prototype:")
}
