package builtin

import (
	"context"
	"encoding/json"
	"fmt"

	"reasonix/internal/tool"
)

func init() { tool.RegisterBuiltin(editFile{}) }

// editFile replaces an exact string in a file. roots confines the target to the
// workspace when non-empty (see writeFile); guard rejects Reasonix session-data
// targets (see SessionDataGuard); workDir, when non-empty, is the directory a
// relative path resolves against (see resolveIn).
type editFile struct {
	roots   []string
	guard   SessionDataGuard
	managed ManagedConfigPaths
	workDir string
}

func (editFile) Name() string { return "edit_file" }

func (editFile) Description() string {
	return "Replace an exact string in a file with another. old_string must occur exactly once; add surrounding context to disambiguate. Use for targeted edits instead of rewriting the whole file."
}

func (editFile) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","description":"File path"},"old_string":{"type":"string","description":"Exact text to replace (must be unique in the file)"},"new_string":{"type":"string","description":"Replacement text (may be empty to delete)"}},"required":["path","old_string","new_string"]}`)
}

func (editFile) ReadOnly() bool { return false }

func (e editFile) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Path      string `json:"path"`
		OldString string `json:"old_string"`
		NewString string `json:"new_string"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if p.Path == "" {
		return "", fmt.Errorf("path is required")
	}
	if p.OldString == "" {
		return "", fmt.Errorf("old_string is required")
	}
	p.Path = resolveIn(e.workDir, p.Path)
	if err := confineWrite(ctx, e.roots, e.guard, e.managed, p.Path); err != nil {
		return "", err
	}

	content, enc, err := readFileEncoded(p.Path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", p.Path, err)
	}

	applied := applyOldStringEdit(content, p.OldString, p.NewString, false)
	switch {
	case applied.applied == 1:
		// ok
	case applied.matches == 0:
		return "", oldStringNotFoundError(p.Path, p.OldString, content)
	default:
		return "", oldStringNotUniqueError(p.Path, p.OldString, content, applied.matches, false)
	}

	if err := writeFileEncoded(p.Path, applied.updated, enc); err != nil {
		return "", fmt.Errorf("write %s: %w", p.Path, err)
	}
	summary := fmt.Sprintf("edited %s", p.Path)
	if applied.fuzzy {
		summary += " (fuzzy match)"
	}
	return withActualPostWriteReceipts(summary, []editReplacementReceipt{applied.receipt}), nil
}
