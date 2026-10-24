package worktree

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

// ValidatePublicationModes rejects metadata the Git-path application protocol
// cannot reproduce. Unsupported changes remain in the persistent root scene.
func ValidatePublicationModes(ctx context.Context, baseline, target Snapshot) error {
	before, err := readSnapshotMetadata(ctx, baseline)
	if err != nil {
		return err
	}
	after, err := readSnapshotMetadata(ctx, target)
	if err != nil {
		return err
	}
	repo, err := target.repo(ctx)
	if err != nil {
		return err
	}
	entries, err := snapshotEntries(ctx, repo, target.Tree)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.Mode != "100644" && entry.Mode != "100755" {
			continue
		}
		mode, exists := after.Files[entry.Path]
		expected := newFileMode()
		if previous, ok := before.Files[entry.Path]; ok {
			expected = previous &^ 0111
		}
		if entry.Mode == "100755" {
			expected |= 0111
		}
		expected = uint32(imageMode(os.FileMode(expected)))
		if !exists || mode != expected {
			return fmt.Errorf("publication cannot reproduce non-Git permission changes at %s; root scene retained", entry.Path)
		}
	}
	for path, mode := range after.Directories {
		expected, exists := before.Directories[path]
		if !exists {
			expected = newDirMode()
		}
		if mode != expected {
			return fmt.Errorf("publication cannot reproduce directory permission changes at %s; root scene retained", path)
		}
	}
	return nil
}

func ValidatePublicationSourceModes(ctx context.Context, baseline, target Snapshot, changes []Change) error {
	before, err := readSnapshotMetadata(ctx, baseline)
	if err != nil {
		return err
	}
	after, err := readSnapshotMetadata(ctx, target)
	if err != nil {
		return err
	}
	for _, change := range changes {
		if _, err := ChangePaths(baseline, []Change{change}); err != nil {
			return err
		}
		info, err := os.Lstat(filepath.Join(baseline.RepoRoot, filepath.FromSlash(change.Path)))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() {
			continue
		}
		mode := uint32(info.Mode().Perm())
		old, wasFile := before.Files[change.Path]
		next, becomesFile := after.Files[change.Path]
		if (!wasFile || mode != old) && (!becomesFile || mode != next) {
			return &ConflictError{Paths: []string{change.Path}}
		}
	}
	return nil
}
