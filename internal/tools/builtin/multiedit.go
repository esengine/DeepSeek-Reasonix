package builtin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"reasonix/internal/contract/tool"
	"reasonix/internal/state/sessiontemp"
)

func init() { tool.RegisterBuiltin(multiEdit{}) }

// multiEdit applies a batch of edits to one file. roots confines the target to
// the workspace when non-empty (see writeFile); guard rejects Reasonix
// session-data targets (see SessionDataGuard); workDir, when non-empty, is the
// directory a relative path resolves against (see resolveIn).
type multiEdit struct {
	roots   []string
	guard   SessionDataGuard
	managed ManagedConfigPaths
	// sessionTemp, when non-nil, adds the session's own temporary directory
	// to the writable surface — the same directory bash writes through $TMPDIR.
	sessionTemp *sessiontemp.Manager
	workDir     string
	overlay     FileOverlay
	views       *FileViews
}

// editStep is one edit in a multi_edit operation. Mirrors edit_file's args
// plus a per-step replace_all toggle so a single call can mix targeted and
// sweep replacements (e.g. rename a function with replace_all, then patch
// one specific call site with a unique-match edit).
type editStep struct {
	OldString  string `json:"old_string"`
	NewString  string `json:"new_string"`
	ReplaceAll bool   `json:"replace_all,omitempty"`
}

func (multiEdit) Name() string { return "multi_edit" }

func (multiEdit) Description() string {
	return "Apply a list of edits to a single file atomically: each edit runs against the result of the previous one, all in memory; the file is rewritten only if every edit succeeds. Cheaper and safer than chaining edit_file calls — a failure in step 3 leaves the file untouched instead of half-edited."
}

func (multiEdit) Schema() json.RawMessage {
	return json.RawMessage(`{
"type":"object",
"properties":{
  "path":{"type":"string","description":"File path"},
  "edits":{
    "type":"array",
    "minItems":1,
    "description":"Ordered edits. Each step sees the file as left by the previous step.",
    "items":{
      "type":"object",
      "properties":{
        "old_string":{"type":"string","description":"Exact text to find. Without replace_all, must match exactly once."},
        "new_string":{"type":"string","description":"Replacement text (empty deletes)."},
        "replace_all":{"type":"boolean","description":"Replace every occurrence instead of requiring uniqueness."}
      },
      "required":["old_string","new_string"]
    }
  }
},
"required":["path","edits"]
}`)
}

func (multiEdit) ReadOnly() bool { return false }

func (multiEdit) WritesNamedPaths() bool { return true }

func (m multiEdit) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Path  string     `json:"path"`
		Edits []editStep `json:"edits"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if p.Path == "" {
		return "", fmt.Errorf("path is required")
	}
	if len(p.Edits) == 0 {
		return "", fmt.Errorf("edits must not be empty")
	}
	p.Path = resolveIn(m.workDir, resolveSessionTemp(m.sessionTemp, p.Path))
	if err := confineWrite(ctx, m.roots, m.guard, m.managed, m.sessionTemp, p.Path); err != nil {
		return "", err
	}

	src, err := readEditSource(ctx, m.overlay, p.Path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", p.Path, err)
	}
	content := src.content

	// A failed step leaves the buffer as it was and the sweep goes on, so one
	// call reports every bad step; any failure still returns before the write.
	applied := 0
	usedFuzzy := false
	receipts := make([]editReplacementReceipt, 0, len(p.Edits))
	var failures []error
	for i, step := range p.Edits {
		if step.OldString == "" {
			failures = append(failures, fmt.Errorf("edit %d: old_string is required", i+1))
			continue
		}
		result := applyOldStringEdit(content, step.OldString, step.NewString, step.ReplaceAll)
		switch {
		case result.applied > 0:
			content = result.updated
			applied += result.applied
			usedFuzzy = usedFuzzy || result.fuzzy
			receipts = append(receipts, result.receipt)
		case result.matches == 0:
			failures = append(failures, fmt.Errorf("edit %d: %w", i+1, oldStringNotFoundError(p.Path, step.OldString, content)))
		default:
			failures = append(failures, fmt.Errorf("edit %d: %w", i+1, oldStringNotUniqueError(p.Path, step.OldString, content, result.matches, true)))
		}
	}
	if err := multiEditFailure(p.Path, len(p.Edits), failures); err != nil {
		return "", err
	}

	if err := src.write(ctx, m.overlay, p.Path, content); err != nil {
		return "", fmt.Errorf("write %s: %w", p.Path, err)
	}
	m.views.saw(p.Path, content)
	summary := fmt.Sprintf("multi_edit %s: %d edits applied (%d total replacements)", p.Path, len(p.Edits), applied)
	if usedFuzzy {
		summary += " (fuzzy match)"
	}
	return withActualPostWriteReceipts(summary, receipts), nil
}

// multiEditFailure keeps a lone failure in its single-step form. Several are
// listed together; steps after a failed one ran without its replacement, so a
// later failure may only be a consequence of an earlier one.
func multiEditFailure(path string, total int, failures []error) error {
	switch len(failures) {
	case 0:
		return nil
	case 1:
		return failures[0]
	}
	return fmt.Errorf("%d of %d edits failed; %s left untouched (each step after a failed one ran without its replacement):\n%w",
		len(failures), total, path, errors.Join(failures...))
}
