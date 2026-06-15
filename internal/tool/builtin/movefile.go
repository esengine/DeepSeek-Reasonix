package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"reasonix/internal/tool"
)

func init() { tool.RegisterBuiltin(moveFile{}) }

var renameFile = os.Rename

// moveFile moves or renames one file. roots, when non-empty, confine both the
// source and destination to the workspace; workDir resolves relative paths.
type moveFile struct {
	roots   []string
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
	if err := confine(m.roots, src); err != nil {
		return "", err
	}
	if err := confine(m.roots, dst); err != nil {
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
		if isCrossDeviceMove(err) {
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

func isCrossDeviceMove(err error) bool {
	var linkErr *os.LinkError
	if !errors.As(err, &linkErr) {
		return false
	}
	msg := strings.ToLower(linkErr.Err.Error())
	return strings.Contains(msg, "cross-device") ||
		strings.Contains(msg, "different device") ||
		strings.Contains(msg, "different disk") ||
		strings.Contains(msg, "not same device")
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
