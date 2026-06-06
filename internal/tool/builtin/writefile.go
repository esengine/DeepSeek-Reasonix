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
// workspace (see confine); the zero value registered at init is unconfined and
// is overridden per run by ConfineWriters. workDir, when non-empty, is the
// directory a relative path resolves against (see resolveIn).
// fileEnc is the project-level encoding override; empty means auto-detect for
// existing files and UTF-8 for new files.
type writeFile struct {
	roots   []string
	workDir string
	fileEnc string // project encoding override (e.g. "GB18030"); empty = auto-detect
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
	if err := confine(w.roots, p.Path); err != nil {
		return "", err
	}

	// Determine the encoding to use when writing.
	// For existing files: detect and preserve the original encoding.
	// For new files: use the project config encoding if set, else UTF-8.
	enc := fileenc.UTF8
	if existing, detEnc, readErr := readFileEncoded(p.Path); readErr == nil {
		// File exists — use its detected encoding for round-trip.
		enc = detEnc
		// No-op check: same content already present?
		if existing == p.Content {
			return fmt.Sprintf("%s already contains the exact content; no changes made", p.Path), nil
		}
	} else if w.fileEnc != "" {
		// New file — honour project encoding setting.
		if forced, ok := fileenc.ParseName(w.fileEnc); ok {
			enc = forced
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
