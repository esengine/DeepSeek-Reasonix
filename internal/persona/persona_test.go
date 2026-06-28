package persona

import (
	"os"
	"path/filepath"
	"testing"
)

func TestListEmpty(t *testing.T) {
	got := List([]string{t.TempDir()})
	if len(got) != 0 {
		t.Errorf("expected empty list, got %d items", len(got))
	}
}

func TestListSingle(t *testing.T) {
	dir := t.TempDir()
	md := "---\nname: helper\ndescription: A helpful persona\nmodel: sonnet\ntools:\n  - read_file\n  - grep\n---\nYou are a helpful assistant."
	if err := os.WriteFile(filepath.Join(dir, "helper.md"), []byte(md), 0o644); err != nil {
		t.Fatal(err)
	}

	got := List([]string{dir})
	if len(got) != 1 {
		t.Fatalf("expected 1 persona, got %d", len(got))
	}
	p := got[0]
	if p.Name != "helper" {
		t.Errorf("Name = %q, want %q", p.Name, "helper")
	}
	if p.Description != "A helpful persona" {
		t.Errorf("Description = %q, want %q", p.Description, "A helpful persona")
	}
	if p.Model != "sonnet" {
		t.Errorf("Model = %q, want %q", p.Model, "sonnet")
	}
	if len(p.Tools) != 2 || p.Tools[0] != "read_file" || p.Tools[1] != "grep" {
		t.Errorf("Tools = %v, want [read_file grep]", p.Tools)
	}
	if p.Body != "You are a helpful assistant." {
		t.Errorf("Body = %q, want %q", p.Body, "You are a helpful assistant.")
	}
	if p.Path != filepath.Join(dir, "helper.md") {
		t.Errorf("Path = %q, want %q", p.Path, filepath.Join(dir, "helper.md"))
	}
}

func TestListOverride(t *testing.T) {
	globalDir := t.TempDir()
	projectDir := t.TempDir()

	if err := os.WriteFile(filepath.Join(globalDir, "coder.md"),
		[]byte("---\nname: coder\ndescription: global coder\n---\nGlobal body"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectDir, "coder.md"),
		[]byte("---\nname: coder\ndescription: project coder\n---\nProject body"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Later dir wins (project overrides global).
	got := List([]string{globalDir, projectDir})
	if len(got) != 1 {
		t.Fatalf("expected 1 persona, got %d: %+v", len(got), got)
	}
	if got[0].Description != "project coder" {
		t.Errorf("expected project coder to override global, got %q", got[0].Description)
	}
	if got[0].Body != "Project body" {
		t.Errorf("expected project body, got %q", got[0].Body)
	}
}

func TestResolve(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "helper.md"),
		[]byte("---\nname: Helper\ndescription: helper persona\n---\nbody"), 0o644); err != nil {
		t.Fatal(err)
	}

	p, ok := Resolve("helper", []string{dir})
	if !ok {
		t.Fatal("Resolve(\"helper\") should succeed")
	}
	if p.Name != "Helper" {
		t.Errorf("Name = %q, want %q", p.Name, "Helper")
	}

	// Case-insensitive lookup.
	p2, ok2 := Resolve("HELPER", []string{dir})
	if !ok2 {
		t.Fatal("Resolve(\"HELPER\") should succeed (case-insensitive)")
	}
	if p2.Name != "Helper" {
		t.Errorf("case-insensitive Resolve returned Name = %q, want %q", p2.Name, "Helper")
	}
}

func TestResolveNotFound(t *testing.T) {
	dir := t.TempDir()
	if _, ok := Resolve("nonexistent", []string{dir}); ok {
		t.Error("Resolve(\"nonexistent\") should return false")
	}
}

func TestResolveEmpty(t *testing.T) {
	dir := t.TempDir()
	if _, ok := Resolve("", []string{dir}); ok {
		t.Error("Resolve(\"\") should return false")
	}
}

func TestApply(t *testing.T) {
	got := Apply("BASE", Persona{Name: "Tester", Body: "Appended text"})
	want := "BASE\n\n# Persona: Tester\n\nAppended text"
	if got != want {
		t.Errorf("Apply = %q, want %q", got, want)
	}
}

func TestApplyEmptyBody(t *testing.T) {
	got := Apply("BASE", Persona{Body: ""})
	if got != "BASE" {
		t.Errorf("Apply with empty body = %q, want %q", got, "BASE")
	}
}
