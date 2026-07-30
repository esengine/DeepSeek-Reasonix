package builtin

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"reasonix/internal/diff"
	"reasonix/internal/tool"
)

func init() { tool.RegisterBuiltin(notebookEdit{}) }

// notebookEdit edits a single cell of a Jupyter notebook (.ipynb). A notebook is
// JSON with a "cells" array; editing it with edit_file means matching escaped
// JSON by hand, which is fragile. This tool targets a cell by index (or id) and
// replaces, inserts, or deletes it, re-serialising so the JSON stays valid and
// unrelated cells, outputs, and top-level metadata are preserved.
//
// roots, when non-empty, confines the target to the workspace (see confine);
// guard rejects Reasonix session-data targets (see SessionDataGuard); the
// zero value registered at init is unconfined and is overridden per run by
// ConfineWriters. workDir, when non-empty, is the directory a relative path
// resolves against (see resolveIn).
type notebookEdit struct {
	roots   []string
	guard   SessionDataGuard
	managed ManagedConfigPaths
	workDir string
}

func (notebookEdit) Name() string { return "notebook_edit" }

func (notebookEdit) ReadOnly() bool { return false }

func (notebookEdit) Description() string {
	return "Edit one cell of a Jupyter notebook (.ipynb). Target a cell by 0-based " +
		"cell_number (or cell_id). edit_mode: \"replace\" (default) swaps the cell's " +
		"source; \"insert\" adds a new cell after cell_number (use -1 to prepend at the " +
		"top), taking cell_type and new_source; \"delete\" removes the cell. cell_type is " +
		"\"code\" or \"markdown\" (required for insert). Editing a code cell clears its " +
		"outputs. Prefer this over edit_file for notebooks — it keeps the JSON valid."
}

func (notebookEdit) Schema() json.RawMessage {
	return json.RawMessage(`{
		"type": "object",
		"properties": {
			"path": {"type": "string", "description": "Path to the .ipynb notebook."},
			"cell_number": {"type": "integer", "description": "0-based index of the target cell. For insert, the new cell goes after this one (-1 prepends)."},
			"cell_id": {"type": "string", "description": "Target the cell by its id instead of cell_number (replace/delete)."},
			"new_source": {"type": "string", "description": "The cell's new source text (replace/insert)."},
			"cell_type": {"type": "string", "enum": ["code", "markdown"], "description": "Cell type for insert (and optional retype on replace)."},
			"edit_mode": {"type": "string", "enum": ["replace", "insert", "delete"], "description": "replace (default), insert, or delete."}
		},
		"required": ["path"]
	}`)
}

type notebookArgs struct {
	Path       string `json:"path"`
	CellNumber *int   `json:"cell_number"`
	CellID     string `json:"cell_id"`
	NewSource  string `json:"new_source"`
	CellType   string `json:"cell_type"`
	EditMode   string `json:"edit_mode"`
}

// notebook is the minimal .ipynb shape we touch. Unknown top-level keys
// (metadata, nbformat, …) and unknown per-cell keys are preserved verbatim via
// json.RawMessage round-tripping.
type notebook struct {
	rest  map[string]json.RawMessage
	cells []map[string]json.RawMessage
}

func (n notebookEdit) Execute(ctx context.Context, raw json.RawMessage) (string, error) {
	a, err := parseNotebookArgs(raw)
	if err != nil {
		return "", err
	}
	a.Path = resolveIn(n.workDir, a.Path)
	if err := confineWrite(ctx, n.roots, n.guard, n.managed, a.Path); err != nil {
		return "", err
	}
	data, err := os.ReadFile(a.Path)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", a.Path, err)
	}
	nb, err := parseNotebook(data)
	if err != nil {
		return "", fmt.Errorf("%s: %w", a.Path, err)
	}

	idx, summary, err := applyNotebookEdit(nb, a)
	if err != nil {
		return "", err
	}

	out, err := nb.marshal()
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(a.Path, out, 0o644); err != nil {
		return "", fmt.Errorf("write %s: %w", a.Path, err)
	}
	return fmt.Sprintf("%s in %s (cell %d; %d cells total)", summary, a.Path, idx, len(nb.cells)), nil
}

// Preview implements tool.Previewer so a checkpoint can snapshot the notebook's
// before/after for rewind. It mirrors Execute's transformation exactly but never
// writes — same arg parsing and targeting rules, so the previewed change equals
// what Execute would persist.
func (n notebookEdit) Preview(raw json.RawMessage) (diff.Change, error) {
	a, err := parseNotebookArgs(raw)
	if err != nil {
		return diff.Change{}, err
	}
	a.Path = resolveIn(n.workDir, a.Path)
	data, err := os.ReadFile(a.Path)
	if err != nil {
		return diff.Change{}, fmt.Errorf("read %s: %w", a.Path, err)
	}
	nb, err := parseNotebook(data)
	if err != nil {
		return diff.Change{}, fmt.Errorf("%s: %w", a.Path, err)
	}
	if _, _, err := applyNotebookEdit(nb, a); err != nil {
		return diff.Change{}, err
	}
	out, err := nb.marshal()
	if err != nil {
		return diff.Change{}, err
	}
	return diff.Build(a.Path, string(data), string(out), diff.Modify), nil
}

func parseNotebookArgs(raw json.RawMessage) (notebookArgs, error) {
	var a notebookArgs
	if err := json.Unmarshal(raw, &a); err != nil {
		return a, fmt.Errorf("invalid args: %w", err)
	}
	// Be forgiving about the source field: models reach for the write_file/edit_file
	// vocabulary ("content"/"source"/"new_string"). Accept those as new_source when
	// new_source itself wasn't given, so a near-miss call succeeds instead of looping.
	if a.NewSource == "" {
		var alias struct {
			Content   string `json:"content"`
			Source    string `json:"source"`
			NewString string `json:"new_string"`
		}
		_ = json.Unmarshal(raw, &alias)
		switch {
		case alias.Content != "":
			a.NewSource = alias.Content
		case alias.Source != "":
			a.NewSource = alias.Source
		case alias.NewString != "":
			a.NewSource = alias.NewString
		}
	}
	if a.Path == "" {
		return a, fmt.Errorf("path is required")
	}
	if a.EditMode == "" {
		a.EditMode = "replace"
	}
	switch a.EditMode {
	case "replace", "insert", "delete":
	default:
		return a, fmt.Errorf("edit_mode must be replace, insert, or delete (got %q)", a.EditMode)
	}
	return a, nil
}

// applyNotebookEdit mutates nb.cells per the args and returns the affected index
// and a one-line summary. Cell targeting is by cell_id when set, else cell_number.
func applyNotebookEdit(nb *notebook, a notebookArgs) (int, string, error) {
	if a.EditMode == "insert" {
		if a.CellType == "" {
			return 0, "", fmt.Errorf("cell_type is required for insert")
		}
		after := -1
		if a.CellNumber != nil {
			after = *a.CellNumber
		}
		if after < -1 || after >= len(nb.cells) {
			return 0, "", fmt.Errorf("cell_number %d out of range for insert (notebook has %d cells; use -1 to prepend)", after, len(nb.cells))
		}
		cell := newCell(a.CellType, a.NewSource)
		at := after + 1 // insert after `after`; -1 → prepend at 0
		nb.cells = append(nb.cells[:at], append([]map[string]json.RawMessage{cell}, nb.cells[at:]...)...)
		return at, "inserted " + a.CellType + " cell", nil
	}

	idx, err := nb.targetIndex(a)
	if err != nil {
		return 0, "", err
	}
	if a.EditMode == "delete" {
		nb.cells = append(nb.cells[:idx], nb.cells[idx+1:]...)
		return idx, "deleted cell", nil
	}
	// replace
	setCellSource(nb.cells[idx], a.NewSource)
	if a.CellType != "" {
		nb.cells[idx]["cell_type"] = jsonString(a.CellType)
	}
	normalizeOutputs(nb.cells[idx], cellTypeOf(nb.cells[idx]))
	return idx, "replaced cell source", nil
}

// targetIndex resolves the cell to act on: cell_id wins when given, else
// cell_number (which must be in range).
func (nb *notebook) targetIndex(a notebookArgs) (int, error) {
	if a.CellID != "" {
		for i, c := range nb.cells {
			if cellID(c) == a.CellID {
				return i, nil
			}
		}
		return 0, fmt.Errorf("no cell with id %q", a.CellID)
	}
	if a.CellNumber == nil {
		// A one-cell notebook is unambiguous: default to cell 0 rather than forcing
		// the caller to restate it. With more than one cell, require an explicit target.
		if len(nb.cells) == 1 {
			return 0, nil
		}
		return 0, fmt.Errorf("cell_number or cell_id is required for %s (notebook has %d cells; pass the 0-based cell_number)", a.EditMode, len(nb.cells))
	}
	n := *a.CellNumber
	if n < 0 || n >= len(nb.cells) {
		return 0, fmt.Errorf("cell_number %d out of range (notebook has %d cells)", n, len(nb.cells))
	}
	return n, nil
}

// parseNotebook decodes just enough of the .ipynb to edit cells while preserving
// every other key (top-level and per-cell) verbatim for re-serialisation.
func parseNotebook(data []byte) (*notebook, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(data, &top); err != nil {
		return nil, fmt.Errorf("not valid notebook JSON: %w", err)
	}
	rawCells, ok := top["cells"]
	if !ok {
		return nil, fmt.Errorf("no \"cells\" array — not a notebook")
	}
	var cells []map[string]json.RawMessage
	if err := json.Unmarshal(rawCells, &cells); err != nil {
		return nil, fmt.Errorf("cells is not an array of objects: %w", err)
	}
	return &notebook{rest: top, cells: cells}, nil
}

// marshal re-serialises the notebook with the edited cells, pretty-printed with
// the one-space indent Jupyter uses, and a trailing newline.
func (nb *notebook) marshal() ([]byte, error) {
	cellsJSON, err := json.Marshal(nb.cells)
	if err != nil {
		return nil, err
	}
	nb.rest["cells"] = cellsJSON
	out, err := json.MarshalIndent(nb.rest, "", " ")
	if err != nil {
		return nil, err
	}
	return append(out, '\n'), nil
}

// newCell builds a fresh cell. Jupyter stores source as an array of lines (each
// ending in \n except the last); outputs/execution_count exist only for code.
func newCell(cellType, source string) map[string]json.RawMessage {
	c := map[string]json.RawMessage{
		"cell_type": jsonString(cellType),
		"metadata":  json.RawMessage(`{}`),
		"source":    sourceLines(source),
	}
	if cellType == "code" {
		c["outputs"] = json.RawMessage(`[]`)
		c["execution_count"] = json.RawMessage(`null`)
	}
	return c
}

func setCellSource(cell map[string]json.RawMessage, source string) {
	cell["source"] = sourceLines(source)
}

// normalizeOutputs makes a cell's output fields match its (possibly just-retyped)
// type: a code cell's stale results are cleared; a markdown cell must not carry
// outputs/execution_count at all, so a code→markdown retype drops them.
func normalizeOutputs(cell map[string]json.RawMessage, cellType string) {
	if cellType == "markdown" {
		delete(cell, "outputs")
		delete(cell, "execution_count")
		return
	}
	cell["outputs"] = json.RawMessage(`[]`)
	cell["execution_count"] = json.RawMessage(`null`)
}

func cellTypeOf(cell map[string]json.RawMessage) string {
	var t string
	_ = json.Unmarshal(cell["cell_type"], &t)
	return t
}

func cellID(cell map[string]json.RawMessage) string {
	raw, ok := cell["id"]
	if !ok {
		return ""
	}
	var id string
	_ = json.Unmarshal(raw, &id)
	return id
}

// sourceLines encodes a string as Jupyter's line-array source form: split on
// newlines, keeping the \n on every line but the last (matching nbformat).
func sourceLines(s string) json.RawMessage {
	if s == "" {
		return json.RawMessage(`[]`)
	}
	parts := strings.SplitAfter(s, "\n")
	// SplitAfter leaves a trailing "" when s ends in \n; drop it.
	if parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	b, _ := json.Marshal(parts)
	return b
}

func jsonString(s string) json.RawMessage {
	b, _ := json.Marshal(s)
	return b
}
