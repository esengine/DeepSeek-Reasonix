package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"reasonix/internal/tool"
)

func init() { tool.RegisterBuiltin(moveFile{}) }

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
	if filepath.Clean(src) == filepath.Clean(dst) {
		return fmt.Sprintf("%s is already at %s; no changes made", src, dst), nil
	}
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
	if _, err := os.Stat(dst); err == nil {
		return "", fmt.Errorf("destination %s already exists", dst)
	} else if !os.IsNotExist(err) {
		return "", fmt.Errorf("stat %s: %w", dst, err)
	}
	if dir := filepath.Dir(dst); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", fmt.Errorf("mkdir %s: %w", dir, err)
		}
	}
	if err := os.Rename(src, dst); err != nil {
		return "", fmt.Errorf("move %s to %s: %w", src, dst, err)
	}
	return fmt.Sprintf("moved %s to %s", src, dst), nil
}
