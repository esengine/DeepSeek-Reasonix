package builtin

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"reasonix/internal/diff"
	fileenc "reasonix/internal/fileutil/encoding"
)

// preview.go gives the file-writing built-ins the optional tool.Previewer
// capability: compute the change a call would make, reading the current file
// but never writing. A front-end (e.g. a desktop approval card) calls Preview
// before the permission gate runs Execute.
//
// Each Preview mirrors its Execute's transformation exactly — same arg parsing,
// same uniqueness / not-found rules — so the previewed NewText equals what
// Execute would persist. That equality is asserted by TestPreviewMatchesExecute
// in preview_test.go, which runs Execute against a temp file and compares; if
// an Execute body ever drifts, that test fails rather than the preview lying.

// Preview computes the change write_file would make. A path that does not yet
// exist is a Create; an existing one is a Modify.
func (w writeFile) Preview(args json.RawMessage) (diff.Change, error) {
	var p struct {
		Path    string `json:"path"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return diff.Change{}, fmt.Errorf("invalid args: %w", err)
	}
	if p.Path == "" {
		return diff.Change{}, fmt.Errorf("path is required")
	}
	p.Path = resolveIn(w.workDir, p.Path)

	old, kind := "", diff.Create
	if data, err := os.ReadFile(p.Path); err == nil {
		enc, _ := fileenc.Detect(data)
		old, kind = string(fileenc.Decode(data, enc)), diff.Modify
	} else if !os.IsNotExist(err) {
		return diff.Change{}, fmt.Errorf("read %s: %w", p.Path, err)
	}
	return diff.Build(p.Path, old, p.Content, kind), nil
}

// Preview computes the change edit_file would make. It enforces the same
// "old_string must occur exactly once" rule as Execute, returning that error
// when it doesn't — so a preview never shows a change the call couldn't make.
func (e editFile) Preview(args json.RawMessage) (diff.Change, error) {
	var p struct {
		Path      string `json:"path"`
		OldString string `json:"old_string"`
		NewString string `json:"new_string"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return diff.Change{}, fmt.Errorf("invalid args: %w", err)
	}
	if p.Path == "" {
		return diff.Change{}, fmt.Errorf("path is required")
	}
	if p.OldString == "" {
		return diff.Change{}, fmt.Errorf("old_string is required")
	}
	p.Path = resolveIn(e.workDir, p.Path)

	content, _, err := readFileEncoded(p.Path)
	if err != nil {
		return diff.Change{}, fmt.Errorf("read %s: %w", p.Path, err)
	}

	switch strings.Count(content, p.OldString) {
	case 0:
		return diff.Change{}, fmt.Errorf("old_string not found in %s", p.Path)
	case 1:
		// ok
	default:
		return diff.Change{}, fmt.Errorf("old_string is not unique in %s; add more surrounding context", p.Path)
	}

	updated := strings.Replace(content, p.OldString, p.NewString, 1)
	return diff.Build(p.Path, content, updated, diff.Modify), nil
}

// Preview computes the change multi_edit would make by replaying every edit
// against an in-memory buffer — exactly as Execute does — and diffing the
// result against the original. Any edit error surfaces here too, so a preview
// of an invalid batch fails the same way the call would.
func (m multiEdit) Preview(args json.RawMessage) (diff.Change, error) {
	var p struct {
		Path  string     `json:"path"`
		Edits []editStep `json:"edits"`
	}
	if err := json.Unmarshal(args, &p); err != nil {
		return diff.Change{}, fmt.Errorf("invalid args: %w", err)
	}
	if p.Path == "" {
		return diff.Change{}, fmt.Errorf("path is required")
	}
	if len(p.Edits) == 0 {
		return diff.Change{}, fmt.Errorf("edits must not be empty")
	}
	p.Path = resolveIn(m.workDir, p.Path)

	content, _, err := readFileEncoded(p.Path)
	if err != nil {
		return diff.Change{}, fmt.Errorf("read %s: %w", p.Path, err)
	}
	original := content

	for i, step := range p.Edits {
		if step.OldString == "" {
			return diff.Change{}, fmt.Errorf("edit %d: old_string is required", i+1)
		}
		if step.ReplaceAll {
			if strings.Count(content, step.OldString) == 0 {
				return diff.Change{}, fmt.Errorf("edit %d: old_string not found", i+1)
			}
			content = strings.ReplaceAll(content, step.OldString, step.NewString)
			continue
		}
		switch strings.Count(content, step.OldString) {
		case 0:
			return diff.Change{}, fmt.Errorf("edit %d: old_string not found", i+1)
		case 1:
			content = strings.Replace(content, step.OldString, step.NewString, 1)
		default:
			return diff.Change{}, fmt.Errorf("edit %d: old_string is not unique; add more surrounding context or set replace_all", i+1)
		}
	}
	return diff.Build(p.Path, original, content, diff.Modify), nil
}
