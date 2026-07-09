package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	fileenc "reasonix/internal/fileutil/encoding"
	"reasonix/internal/tool"
)

func init() { tool.RegisterBuiltin(writeFile{}) }

// writeFile writes a file. roots, when non-empty, confines the target to the
// workspace (see confine); guard rejects Reasonix session-data targets even
// inside the roots (see SessionDataGuard); the zero value registered at init is
// unconfined and is overridden per run by ConfineWriters. workDir, when
// non-empty, is the directory a relative path resolves against (see resolveIn).
type writeFile struct {
	roots   []string
	guard   SessionDataGuard
	managed ManagedConfigPaths
	workDir string
	// overlay, when non-nil, routes the write through the host transport so an
	// open editor buffer updates too. Consulted only after write confinement,
	// and only for plain-UTF-8 targets (the overlay is text-only, so non-UTF-8
	// files keep the local encoding-preserving path).
	overlay FileOverlay
}

func (writeFile) Name() string { return "write_file" }

func (writeFile) Description() string {
	return "Write content to a file at the given path (overwriting existing content). Creates parent directories as needed."
}

func (writeFile) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"File path"},"content":{"type":"string","description":"Full content to write"}},"required":["path","content"]}`)
}

func (writeFile) ReadOnly() bool { return false }

func (w writeFile) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if p.Path == "" {
		return "", fmt.Errorf("path is required")
	}
	p.Path = resolveIn(w.workDir, p.Path)
	if err := confineWrite(ctx, w.roots, w.guard, w.managed, p.Path); err != nil {
		return "", err
	}
	// Preserve the existing file's encoding (GBK/UTF-16/BOM) on overwrite instead
	// of always writing UTF-8, which would silently corrupt a non-UTF-8 file.
	// readFileEncoded returns enc=UTF8 for a missing file — the right default for
	// a newly created one.
	existing, enc, rerr := readFileEncoded(p.Path)
	if rerr == nil && existing == p.Content {
		return fmt.Sprintf("%s already contains the exact content; no changes made", p.Path), nil
	}
	// The host overlay applies the write to the editor buffer and the file in
	// one step. Text-only, so it handles plain UTF-8 targets (and new files);
	// non-UTF-8 files stay on the local encoding-preserving path below.
	if w.overlay != nil && filepath.IsAbs(p.Path) && (rerr != nil || enc == fileenc.UTF8) {
		if ok, werr := w.overlay.WriteTextFile(ctx, p.Path, p.Content); ok {
			if werr != nil {
				return "", fmt.Errorf("write %s: %w", p.Path, werr)
			}
			return fmt.Sprintf("wrote %d bytes to %s", len(p.Content), p.Path), nil
		}
	}
	if dir := filepath.Dir(p.Path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", fmt.Errorf("mkdir %s: %w", dir, err)
		}
	}
	if err := writeFileEncoded(p.Path, p.Content, enc); err != nil {
		return "", fmt.Errorf("write %s: %w", p.Path, err)
	}
	return fmt.Sprintf("wrote %d bytes to %s", len(p.Content), p.Path), nil
}
