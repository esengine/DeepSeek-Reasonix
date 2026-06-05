package builtin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// sampleNotebook is a minimal but realistic .ipynb: a markdown cell and a code
// cell with an output + execution_count, plus top-level metadata/nbformat that
// must survive a round-trip.
const sampleNotebook = `{
 "cells": [
  {"cell_type": "markdown", "id": "intro", "metadata": {}, "source": ["# Title\n", "text"]},
  {"cell_type": "code", "id": "c1", "metadata": {}, "execution_count": 5, "outputs": [{"output_type": "stream", "text": "old"}], "source": ["print(1)\n"]}
 ],
 "metadata": {"kernelspec": {"name": "python3"}},
 "nbformat": 4,
 "nbformat_minor": 5
}`

func writeNotebook(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "nb.ipynb")
	if err := os.WriteFile(p, []byte(sampleNotebook), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func runNotebookEdit(t *testing.T, path string, args map[string]any) (string, error) {
	t.Helper()
	args["path"] = path
	raw, _ := json.Marshal(args)
	return notebookEdit{}.Execute(context.Background(), raw)
}

func readCells(t *testing.T, path string) []map[string]json.RawMessage {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	nb, err := parseNotebook(data)
	if err != nil {
		t.Fatalf("result is not valid notebook JSON: %v", err)
	}
	return nb.cells
}

func TestNotebookReplaceBySource(t *testing.T) {
	p := writeNotebook(t)
	if _, err := runNotebookEdit(t, p, map[string]any{"cell_number": 1, "new_source": "print(42)\n"}); err != nil {
		t.Fatal(err)
	}
	cells := readCells(t, p)
	if len(cells) != 2 {
		t.Fatalf("replace changed cell count: %d", len(cells))
	}
	if got := string(cells[1]["source"]); !strings.Contains(got, "print(42)") {
		t.Errorf("source not replaced: %s", got)
	}
	// Editing a code cell clears its outputs + execution_count.
	if got := string(cells[1]["outputs"]); got != "[]" {
		t.Errorf("outputs not cleared: %s", got)
	}
	if got := string(cells[1]["execution_count"]); got != "null" {
		t.Errorf("execution_count not cleared: %s", got)
	}
}

func TestNotebookRetypeNormalizesOutputs(t *testing.T) {
	p := writeNotebook(t)
	if _, err := runNotebookEdit(t, p, map[string]any{"cell_number": 1, "cell_type": "markdown", "new_source": "# now md"}); err != nil {
		t.Fatal(err)
	}
	md := readCells(t, p)[1]
	if _, has := md["outputs"]; has {
		t.Errorf("code→markdown left 'outputs' (invalid nbformat): %s", md["outputs"])
	}
	if _, has := md["execution_count"]; has {
		t.Errorf("code→markdown left 'execution_count' (invalid nbformat): %s", md["execution_count"])
	}

	if _, err := runNotebookEdit(t, p, map[string]any{"cell_number": 0, "cell_type": "code", "new_source": "y = 2\n"}); err != nil {
		t.Fatal(err)
	}
	code := readCells(t, p)[0]
	if string(code["outputs"]) != "[]" || string(code["execution_count"]) != "null" {
		t.Errorf("markdown→code missing output scaffolding: outputs=%s exec=%s", code["outputs"], code["execution_count"])
	}
}

func TestNotebookReplaceByID(t *testing.T) {
	p := writeNotebook(t)
	if _, err := runNotebookEdit(t, p, map[string]any{"cell_id": "intro", "new_source": "# New"}); err != nil {
		t.Fatal(err)
	}
	cells := readCells(t, p)
	if got := string(cells[0]["source"]); !strings.Contains(got, "# New") {
		t.Errorf("cell_id target not replaced: %s", got)
	}
}

func TestNotebookInsertAfter(t *testing.T) {
	p := writeNotebook(t)
	if _, err := runNotebookEdit(t, p, map[string]any{"edit_mode": "insert", "cell_number": 0, "cell_type": "code", "new_source": "x = 1\n"}); err != nil {
		t.Fatal(err)
	}
	cells := readCells(t, p)
	if len(cells) != 3 {
		t.Fatalf("insert should add a cell, got %d", len(cells))
	}
	if got := string(cells[1]["cell_type"]); got != `"code"` {
		t.Errorf("inserted cell type wrong: %s", got)
	}
	if got := string(cells[1]["source"]); !strings.Contains(got, "x = 1") {
		t.Errorf("inserted source wrong: %s", got)
	}
	// A code cell gets outputs/execution_count scaffolding.
	if _, ok := cells[1]["outputs"]; !ok {
		t.Error("inserted code cell missing outputs")
	}
}

func TestNotebookInsertPrepend(t *testing.T) {
	p := writeNotebook(t)
	if _, err := runNotebookEdit(t, p, map[string]any{"edit_mode": "insert", "cell_number": -1, "cell_type": "markdown", "new_source": "top"}); err != nil {
		t.Fatal(err)
	}
	cells := readCells(t, p)
	if got := string(cells[0]["source"]); !strings.Contains(got, "top") {
		t.Errorf("prepend should land at index 0: %s", got)
	}
}

func TestNotebookDelete(t *testing.T) {
	p := writeNotebook(t)
	if _, err := runNotebookEdit(t, p, map[string]any{"edit_mode": "delete", "cell_number": 0}); err != nil {
		t.Fatal(err)
	}
	cells := readCells(t, p)
	if len(cells) != 1 {
		t.Fatalf("delete should leave 1 cell, got %d", len(cells))
	}
	if got := cellID(cells[0]); got != "c1" {
		t.Errorf("wrong cell deleted; remaining id = %q", got)
	}
}

func TestNotebookPreservesTopLevelKeys(t *testing.T) {
	p := writeNotebook(t)
	if _, err := runNotebookEdit(t, p, map[string]any{"cell_number": 0, "new_source": "# x"}); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(p)
	var top map[string]json.RawMessage
	if err := json.Unmarshal(data, &top); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"metadata", "nbformat", "nbformat_minor"} {
		if _, ok := top[k]; !ok {
			t.Errorf("top-level key %q lost on round-trip", k)
		}
	}
}

func TestNotebookErrors(t *testing.T) {
	p := writeNotebook(t)
	if _, err := runNotebookEdit(t, p, map[string]any{"cell_number": 9, "new_source": "x"}); err == nil {
		t.Error("out-of-range cell_number should error")
	}
	if _, err := runNotebookEdit(t, p, map[string]any{"cell_id": "nope", "edit_mode": "delete"}); err == nil {
		t.Error("unknown cell_id should error")
	}
	if _, err := runNotebookEdit(t, p, map[string]any{"edit_mode": "insert", "cell_number": 0, "new_source": "x"}); err == nil {
		t.Error("insert without cell_type should error")
	}
	if _, err := runNotebookEdit(t, p, map[string]any{"edit_mode": "bogus"}); err == nil {
		t.Error("bad edit_mode should error")
	}
}

// TestNotebookPreviewMatchesExecute checks the Previewer mirrors Execute: the
// previewed NewText equals the file content Execute persists.
func TestNotebookPreviewMatchesExecute(t *testing.T) {
	p := writeNotebook(t)
	args := map[string]any{"path": p, "cell_number": 1, "new_source": "print(99)\n"}
	raw, _ := json.Marshal(args)

	change, err := notebookEdit{}.Preview(raw)
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if _, err := (notebookEdit{}).Execute(context.Background(), raw); err != nil {
		t.Fatalf("execute: %v", err)
	}
	persisted, _ := os.ReadFile(p)
	if change.NewText != string(persisted) {
		t.Errorf("preview NewText != persisted content:\npreview:\n%s\npersisted:\n%s", change.NewText, persisted)
	}
}

// TestNotebookContentAlias accepts the write_file-style "content" field as an
// alias for new_source, and defaults to the only cell when no target is given —
// the near-miss shape a model reaches for, which should succeed not loop.
func TestNotebookContentAlias(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "one.ipynb")
	one := `{"cells":[{"cell_type":"code","id":"a","metadata":{},"execution_count":null,"outputs":[],"source":["print('hi')\n"]}],"metadata":{},"nbformat":4,"nbformat_minor":5}`
	if err := os.WriteFile(p, []byte(one), 0o644); err != nil {
		t.Fatal(err)
	}
	// {content, path} with no cell_number — the exact shape that looped before.
	if _, err := runNotebookEdit(t, p, map[string]any{"content": "print(\"world\")\n"}); err != nil {
		t.Fatalf("content-alias single-cell replace should succeed, got: %v", err)
	}
	cells := readCells(t, p)
	if got := string(cells[0]["source"]); !strings.Contains(got, `print(\"world\")`) {
		t.Errorf("alias source not applied: %s", got)
	}
}

// TestNotebookMissingTargetMultiCell still requires an explicit target when the
// notebook is ambiguous (more than one cell), with an instructive message.
func TestNotebookMissingTargetMultiCell(t *testing.T) {
	p := writeNotebook(t) // 2 cells
	_, err := runNotebookEdit(t, p, map[string]any{"new_source": "x"})
	if err == nil {
		t.Fatal("multi-cell replace with no target should error")
	}
	if !strings.Contains(err.Error(), "cell_number") {
		t.Errorf("error should name cell_number: %v", err)
	}
}

// TestNotebookSourceLines checks the nbformat line-array encoding: \n kept on
// every line but the last.
func TestNotebookSourceLines(t *testing.T) {
	if got := string(sourceLines("a\nb\n")); got != `["a\n","b\n"]` {
		t.Errorf("source line encoding = %s", got)
	}
	if got := string(sourceLines("solo")); got != `["solo"]` {
		t.Errorf("single line = %s", got)
	}
	if got := string(sourceLines("")); got != `[]` {
		t.Errorf("empty = %s", got)
	}
}
