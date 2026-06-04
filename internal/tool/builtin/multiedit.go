package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"reasonix/internal/tool"
)

func init() { tool.RegisterBuiltin(multiEdit{}) }

// multiEdit applies a batch of edits to one file. roots confines the target to
// the workspace when non-empty (see writeFile); workDir, when non-empty, is the
// directory a relative path resolves against (see resolveIn).
type multiEdit struct {
	roots        []string
	workDir      string
	fileEncoding string // project-level encoding override; empty = auto-detect
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
  },
  "encoding":{"type":"string","description":"File encoding override (e.g. \"UTF-8\", \"GB18030\"). When omitted, auto-detection is used."}
},
"required":["path","edits"]
}`)
}

func (multiEdit) ReadOnly() bool { return false }

func (m multiEdit) Execute(ctx context.Context, args json.RawMessage) (string, error) {
	var p struct {
		Path     string     `json:"path"`
		Edits    []editStep `json:"edits"`
		Encoding string     `json:"encoding,omitempty"`
	}
	if err := unmarshalArgs(args, &p); err != nil {
		return "", fmt.Errorf("invalid args: %w", err)
	}
	if p.Path == "" {
		return "", fmt.Errorf("path is required")
	}
	if len(p.Edits) == 0 {
		return "", fmt.Errorf("edits must not be empty")
	}
	p.Path = resolveIn(m.workDir, p.Path)
	if err := confine(m.roots, p.Path); err != nil {
		return "", err
	}

	encParam := p.Encoding
	if encParam == "" {
		encParam = m.fileEncoding
	}
	content, enc, err := readFileEncodedWith(p.Path, encParam)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", p.Path, err)
	}

	// Apply edits in order against the running in-memory buffer. Any failure
	// returns before the write, leaving the file untouched — that's the
	// safety guarantee that makes multi_edit preferable to chained
	// edit_file calls.
	applied := 0
	for i, step := range p.Edits {
		if step.OldString == "" {
			return "", fmt.Errorf("edit %d: old_string is required", i+1)
		}
		old, newStr := matchLineEndings(content, step.OldString, step.NewString)
		if step.ReplaceAll {
			count := strings.Count(content, old)
			if count == 0 {
				// Fuzzy match for replace_all: find the actual text and replace all.
				if actual, ok := fuzzyFind(content, old); ok {
					_, newStr = matchLineEndings(content, old, newStr)
					fuzzyCount := strings.Count(content, actual)
					content = strings.ReplaceAll(content, actual, newStr)
					applied += fuzzyCount
					continue
				}
				return "", diagnoseNotFound(p.Path, step.OldString, content)
			}
			content = strings.ReplaceAll(content, old, newStr)
			applied += count
			continue
		}
		switch strings.Count(content, old) {
		case 0:
			// Fuzzy fallback.
			if actual, ok := fuzzyFind(content, old); ok {
				_, newStr = matchLineEndings(content, old, newStr)
				content = strings.Replace(content, actual, newStr, 1)
				applied++
				continue
			}
			return "", diagnoseNotFound(p.Path, step.OldString, content)
		case 1:
			content = strings.Replace(content, old, newStr, 1)
			applied++
		default:
			return "", diagnoseNotUnique(p.Path, old, content)
		}
	}

	if err := writeFileEncodedWith(p.Path, content, enc, encParam); err != nil {
		return "", fmt.Errorf("write %s: %w", p.Path, err)
	}
	return fmt.Sprintf("multi_edit %s: %d edits applied (%d total replacements)", p.Path, len(p.Edits), applied), nil
}
