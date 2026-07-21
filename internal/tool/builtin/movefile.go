package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"reasonix/internal/diff"
	"reasonix/internal/fileutil"
	"reasonix/internal/tool"
)

func init() { tool.RegisterBuiltin(moveFile{}) }

var renameFile = os.Rename

// moveFile moves or renames one file. roots, when non-empty, confine both the
// source and destination to the workspace; guard rejects Reasonix session-data
// endpoints on either side (a move out of the store mutates it too); workDir
// resolves relative paths.
type moveFile struct {
	roots   []string
	guard   SessionDataGuard
	managed ManagedConfigPaths
	workDir string
}

func (moveFile) Name() string { return "move_file" }

func (moveFile) Description() string {
	return "Move or rename a file from source_path to destination_path. Creates the destination parent directory as needed. Use instead of shell mv, Move-Item, or ren for file moves so workspace confinement and file-edit permissions apply."
}

func (moveFile) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"source_path":{"type":"string","description":"Existing file path to move"},"destination_path":{"type":"string","description":"Destination file path; must not already exist"}},"required":["source_path","destination_path"]}`)
}

func (moveFile) ReadOnly() bool { return false }

// Preview computes the rename change move_file would make, without touching
// disk. It mirrors Execute's arg parsing, path resolution, workspace
// confinement, and source-existence checks so a UI can render "src → dst" for
// an approval card. It enforces confinement (via the ctx-less confinePreview,
// like the other writer Previews) so a call targeting a path outside the
// workspace never stats it or leaks its state into the approval prompt; it does
// not write, create the destination, or remove the source.
func (m moveFile) Preview(args json.RawMessage) (diff.Change, error) {
	return m.preview(args, tool.PendingBatchState{})
}

// PreviewInBatch previews a rename against the effects earlier calls in the same
// batch will have on disk. A chained rename (a→b; b→c) can't stat its source at
// preview time because the earlier move hasn't run; consulting pending lets the
// second call still render its "src → dst" card. Implements tool.BatchPreviewer.
func (m moveFile) PreviewInBatch(args json.RawMessage, pending tool.PendingBatchState) (diff.Change, error) {
	return m.preview(args, pending)
}

func (m moveFile) preview(args json.RawMessage, pending tool.PendingBatchState) (diff.Change, error) {
	var p struct {
		SourcePath      string `json:"source_path"`
		DestinationPath string `json:"destination_path"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return diff.Change{}, fmt.Errorf("invalid args: %w", err)
	}
	if p.SourcePath == "" {
		return diff.Change{}, fmt.Errorf("source_path is required")
	}
	if p.DestinationPath == "" {
		return diff.Change{}, fmt.Errorf("destination_path is required")
	}
	src := resolveIn(m.workDir, p.SourcePath)
	dst := resolveIn(m.workDir, p.DestinationPath)
	if err := confinePreview(m.roots, m.guard, m.managed, src); err != nil {
		return diff.Change{}, err
	}
	if err := confinePreview(m.roots, m.guard, m.managed, dst); err != nil {
		return diff.Change{}, err
	}
	// src == dst is a no-op for Execute ("no changes made"); return an empty
	// Change so no rename card or checkpoint snapshot is produced for a move
	// that never happens. Checked before the stat so a same-path no-op needs no
	// on-disk source.
	if filepath.Clean(src) == filepath.Clean(dst) {
		return diff.Change{}, nil
	}
	// A source an earlier batch call will create (a chained rename's middle
	// path) is absent on disk now but present at execute time: render the card
	// from the pending state instead of stat-ing a not-yet-existing file. The
	// earlier call already validated it was a regular file, and Execute
	// re-checks everything, so skipping the dir/exists checks here only affects
	// the speculative preview card.
	if pending.Created[filepath.Clean(src)] {
		return diff.BuildRename(src, dst), nil
	}
	info, err := os.Stat(src)
	if err != nil {
		return diff.Change{}, fmt.Errorf("stat %s: %w", src, err)
	}
	if info.IsDir() {
		return diff.Change{}, fmt.Errorf("%s is a directory; move_file only moves files", src)
	}
	// A dst an earlier batch call will remove (renamed away, or its source) is
	// free at execute time even if it exists now: treat it as absent so the
	// preview doesn't flag a false "destination already exists".
	if pending.Removed[filepath.Clean(dst)] {
		return diff.BuildRename(src, dst), nil
	}
	if dstInfo, err := os.Stat(dst); err == nil {
		if !os.SameFile(info, dstInfo) {
			return diff.Change{}, fmt.Errorf("destination %s already exists", dst)
		}
	} else if !os.IsNotExist(err) {
		return diff.Change{}, fmt.Errorf("stat %s: %w", dst, err)
	}
	return diff.BuildRename(src, dst), nil
}

func (m moveFile) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		SourcePath      string `json:"source_path"`
		DestinationPath string `json:"destination_path"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if p.SourcePath == "" {
		return "", fmt.Errorf("source_path is required")
	}
	if p.DestinationPath == "" {
		return "", fmt.Errorf("destination_path is required")
	}
	src := resolveIn(m.workDir, p.SourcePath)
	dst := resolveIn(m.workDir, p.DestinationPath)
	if err := confineWrite(ctx, m.roots, m.guard, m.managed, src); err != nil {
		return "", err
	}
	if err := confineWrite(ctx, m.roots, m.guard, m.managed, dst); err != nil {
		return "", err
	}
	info, err := os.Stat(src)
	if err != nil {
		return "", fmt.Errorf("stat %s: %w", src, err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("%s is a directory; move_file only moves files", src)
	}
	if filepath.Clean(src) == filepath.Clean(dst) {
		return fmt.Sprintf("%s is already at %s; no changes made", src, dst), nil
	}
	sameFileDestination := false
	if dstInfo, err := os.Stat(dst); err == nil {
		if !os.SameFile(info, dstInfo) {
			return "", fmt.Errorf("destination %s already exists", dst)
		}
		sameFileDestination = true
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("stat %s: %w", dst, err)
	}
	if dir := filepath.Dir(dst); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", fmt.Errorf("mkdir %s: %w", dir, err)
		}
	}
	if err := renameFile(src, dst); err != nil {
		if sameFileDestination {
			if rerr := renameSameFileDestination(src, dst); rerr != nil {
				return "", fmt.Errorf("move %s to %s: %w", src, dst, rerr)
			}
			return fmt.Sprintf("moved %s to %s", src, dst), nil
		}
		if fileutil.IsCrossDeviceError(err) {
			if cerr := copyRegularFileAndRemoveSource(src, dst, info); cerr != nil {
				return "", fmt.Errorf("move %s to %s: %w", src, dst, cerr)
			}
			return fmt.Sprintf("moved %s to %s", src, dst), nil
		}
		return "", fmt.Errorf("move %s to %s: %w", src, dst, err)
	}
	return fmt.Sprintf("moved %s to %s", src, dst), nil
}

func renameSameFileDestination(src, dst string) error {
	tmp, err := os.CreateTemp(filepath.Dir(src), ".reasonix-move-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if err := tmp.Close(); err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Remove(tmpName); err != nil {
		return err
	}

	if err := renameFile(src, tmpName); err != nil {
		return err
	}
	if err := os.Remove(dst); err != nil && !os.IsNotExist(err) {
		if restoreErr := renameFile(tmpName, src); restoreErr != nil {
			return fmt.Errorf("%w; restore %s: %v", err, src, restoreErr)
		}
		return err
	}
	if err := renameFile(tmpName, dst); err != nil {
		if restoreErr := renameFile(tmpName, src); restoreErr != nil {
			return fmt.Errorf("%w; restore %s: %v", err, src, restoreErr)
		}
		return err
	}
	return nil
}

func copyRegularFileAndRemoveSource(src, dst string, info os.FileInfo) error {
	if !info.Mode().IsRegular() {
		return fmt.Errorf("cross-filesystem fallback only supports regular files")
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode().Perm())
	if err != nil {
		return err
	}
	removeDst := true
	defer func() {
		if removeDst {
			_ = os.Remove(dst)
		}
	}()
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}
	if err := in.Close(); err != nil {
		return err
	}
	if err := os.Remove(src); err != nil {
		return err
	}
	removeDst = false
	return nil
}
