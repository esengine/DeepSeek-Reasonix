package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	fileenc "reasonix/internal/fileutil/encoding"
	"reasonix/internal/sandbox"
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
	rootSet *sandbox.WritableRootSet
	guard   SessionDataGuard
	managed ManagedConfigPaths
	workDir string
	// overlay, when non-nil, routes the write through the host transport so an
	// open editor buffer updates too. Consulted only after write confinement,
	// and only for plain-UTF-8 targets (the overlay is text-only, so non-UTF-8
	// files keep the local encoding-preserving path).
	overlay FileOverlay
	// receipt is an optional per-runtime effect hook. hadPrior means an existing
	// file was overwritten; prior is its previous content.
	receipt func(path string, hadPrior bool, prior []byte)
}

func (writeFile) Name() string { return "write_file" }

func (writeFile) Description() string {
	return "Write content to a file at the given path (overwriting existing content). Creates parent directories as needed."
}

func (writeFile) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"File path"},"content":{"type":"string","description":"Full content to write"}},"required":["path","content"]}`)
}

func (writeFile) ReadOnly() bool { return false }

func (w writeFile) DeclareWriteAccess(args json.RawMessage) (tool.WriteAccessDeclaration, error) {
	return declareFilePathWriteAccess(w.workDir, args)
}

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
	if err := confineWrite(ctx, effectiveWriteRoots(ctx, w.rootSet, w.roots), w.guard, w.managed, p.Path); err != nil {
		return "", err
	}
	// Preserve the existing file's encoding (GBK/UTF-16/BOM) on overwrite instead
	// of always writing UTF-8, which would silently corrupt a non-UTF-8 file. A
	// missing file yields enc=UTF8 — the right default for a new one. Reading via
	// the overlay makes the no-op check see the same buffer Preview does.
	src, rerr := readEditSource(ctx, w.overlay, p.Path)
	if rerr == nil && src.content == p.Content {
		return fmt.Sprintf("%s already contains the exact content; no changes made", p.Path), nil
	}
	if rerr != nil && !os.IsNotExist(rerr) {
		return "", rerr
	}
	if err := src.assertUnchanged(ctx, w.overlay, p.Path); err != nil {
		return "", err
	}
	// The host overlay applies the write to the editor buffer and the file in
	// one step. Text-only, so it handles plain UTF-8 targets (and new files);
	// non-UTF-8 files stay on the local encoding-preserving path below.
	if w.overlay != nil && filepath.IsAbs(p.Path) && (rerr != nil || src.enc == fileenc.UTF8) {
		if err := src.recordWrite(ctx, p.Path, p.Content, "overlay", w.overlay); err != nil {
			return "", err
		}
		if err := src.assertUnchanged(ctx, w.overlay, p.Path); err != nil {
			return "", err
		}
		if ok, werr := w.overlay.WriteTextFile(ctx, p.Path, p.Content); ok {
			if werr != nil {
				if tool.HasWriteIntentHook(ctx) {
					return "", fmt.Errorf("write outcome unknown: %w", werr)
				}
				return "", fmt.Errorf("write %s: %w", p.Path, werr)
			}
			if w.receipt != nil {
				w.receipt(p.Path, rerr == nil, []byte(src.content))
			}
			return fmt.Sprintf("wrote %d bytes to %s", len(p.Content), p.Path), nil
		}
		if tool.HasWriteIntentHook(ctx) {
			return "", fmt.Errorf("write outcome unknown: original overlay did not confirm the write")
		}
	}
	if err := src.recordWrite(ctx, p.Path, p.Content, "disk", w.overlay); err != nil {
		return "", err
	}
	if err := src.assertUnchanged(ctx, w.overlay, p.Path); err != nil {
		return "", err
	}
	if dir := filepath.Dir(p.Path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return "", fmt.Errorf("mkdir %s: %w", dir, err)
		}
	}
	hadPrior := rerr == nil
	var prior []byte
	if hadPrior {
		prior = []byte(src.content)
	}
	if err := writeFileEncoded(p.Path, p.Content, src.enc); err != nil {
		return "", fmt.Errorf("write %s: %w", p.Path, err)
	}
	if w.receipt != nil {
		w.receipt(p.Path, hadPrior, prior)
	}
	return fmt.Sprintf("wrote %d bytes to %s", len(p.Content), p.Path), nil
}

// BindFileWriteReceipt returns t with a per-runtime write receipt callback when
// t is write_file. Other tools are returned unchanged.
func BindFileWriteReceipt(t tool.Tool, receipt func(path string, hadPrior bool, prior []byte)) tool.Tool {
	w, ok := t.(writeFile)
	if !ok {
		return t
	}
	w.receipt = receipt
	return w
}
