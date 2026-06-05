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
type writeFile struct {
	roots        []string
	workDir      string
	fileEncoding string // project-level encoding override; empty = auto-detect from existing file
}

func (writeFile) Name() string { return "write_file" }

func (writeFile) Description() string {
	return "Write content to a file at the given path (overwriting existing content). Creates parent directories as needed. When the target file already exists and no encoding is specified, the original file encoding is preserved."
}

func (writeFile) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"File path"},"content":{"type":"string","description":"Full content to write"},"encoding":{"type":"string","description":"Target encoding (e.g. \"UTF-8\", \"GB18030\", \"UTF-16 LE\"). Defaults to the existing file's encoding, or UTF-8 for new files."}},"required":["path","content"]}`)
}

func (writeFile) ReadOnly() bool { return false }

func (w writeFile) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Path     string `json:"path"`
		Content  string `json:"content"`
		Encoding string `json:"encoding,omitempty"`
	}
	if err := unmarshalArgs(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if p.Path == "" {
		return "", fmt.Errorf("path is required")
	}
	p.Path = resolveIn(w.workDir, p.Path)
	if err := confine(w.roots, p.Path); err != nil {
		return "", err
	}
	if dir := filepath.Dir(p.Path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", fmt.Errorf("mkdir %s: %w", dir, err)
		}
	}

	// Determine target encoding: explicit override > project encoding > existing
	// file encoding > UTF-8 (new files).
	encParam := p.Encoding
	if encParam == "" {
		encParam = w.fileEncoding
	}
	var targetEnc fileenc.Kind
	if forced, ok := fileenc.ParseName(encParam); ok {
		targetEnc = forced
	} else if existing, err := os.ReadFile(p.Path); err == nil {
		targetEnc, _ = fileenc.Detect(existing)
	} else {
		targetEnc = fileenc.UTF8
	}

	data := fileenc.Encode(p.Content, targetEnc)
	if err := os.WriteFile(p.Path, data, 0o644); err != nil {
		return "", fmt.Errorf("write %s: %w", p.Path, err)
	}
	return fmt.Sprintf("wrote %d bytes to %s (%s)", len(data), p.Path, fileenc.Name(targetEnc)), nil
}
