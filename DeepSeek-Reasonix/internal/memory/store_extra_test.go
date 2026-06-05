package memory

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- splitFrontmatter ---

func TestSplitFrontmatterNoFence(t *testing.T) {
	fm, body := splitFrontmatter("just plain text\nno frontmatter")
	if len(fm) != 0 {
		t.Errorf("expected empty fm, got %v", fm)
	}
	if !strings.Contains(body, "just plain text") {
		t.Errorf("body should contain original text: %q", body)
	}
}

func TestSplitFrontmatterUnclosedFence(t *testing.T) {
	input := "---\nname: test\ndescription: desc\n\nsome body without closing fence"
	fm, body := splitFrontmatter(input)
	// Unclosed fence: treat all as body.
	if len(fm) != 0 {
		t.Errorf("unclosed fence should return empty fm, got %v", fm)
	}
	if !strings.Contains(body, "---") {
		t.Errorf("body should contain the original content: %q", body)
	}
}

func TestSplitFrontmatterEmptyBody(t *testing.T) {
	input := "---\nname: test\n---\n"
	fm, body := splitFrontmatter(input)
	if fm["name"] != "test" {
		t.Errorf("name = %q", fm["name"])
	}
	if strings.TrimSpace(body) != "" {
		t.Errorf("expected empty body, got %q", body)
	}
}

func TestSplitFrontmatterNestedMetadata(t *testing.T) {
	input := "---\nname: my-fact\ndescription: a desc\nmetadata:\n  type: user\n---\n\nbody here"
	fm, body := splitFrontmatter(input)
	if fm["name"] != "my-fact" {
		t.Errorf("name = %q", fm["name"])
	}
	if fm["description"] != "a desc" {
		t.Errorf("description = %q", fm["description"])
	}
	// The nested "  type: user" should flatten to fm["type"].
	if fm["type"] != "user" {
		t.Errorf("type = %q, expected flattened from metadata", fm["type"])
	}
	if !strings.Contains(body, "body here") {
		t.Errorf("body = %q", body)
	}
}

func TestSplitFrontmatterCRLF(t *testing.T) {
	input := "---\r\nname: test\r\n---\r\nbody\r\n"
	fm, body := splitFrontmatter(input)
	if fm["name"] != "test" {
		t.Errorf("name = %q", fm["name"])
	}
	if !strings.Contains(body, "body") {
		t.Errorf("body = %q", body)
	}
}

func TestSplitFrontmatterQuotedValues(t *testing.T) {
	input := "---\nname: test\ndescription: \"quoted desc\"\n---\n"
	fm, _ := splitFrontmatter(input)
	if fm["description"] != "quoted desc" {
		t.Errorf("description should be unquoted: %q", fm["description"])
	}
}

// --- slug ---

func TestSlug(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"Prefers Tabs", "prefers-tabs"},
		{"  spaces  ", "spaces"},
		{"CamelCase", "camelcase"},
		{"with/slash", "with-slash"},
		{"", ""},
		{"---", ""},
		{"hello_world", "hello-world"},
	}
	for _, c := range cases {
		got := slug(c.input)
		if got != c.want {
			t.Errorf("slug(%q) = %q, want %q", c.input, got, c.want)
		}
	}
}

// --- oneLine ---

func TestOneLine(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"hello world", "hello world"},
		{"  multiple   spaces  ", "multiple spaces"},
		{"tabs\there", "tabs here"},
		{"\n\nnewlines\n\n", "newlines"},
		{"", ""},
	}
	for _, c := range cases {
		got := oneLine(c.input)
		if got != c.want {
			t.Errorf("oneLine(%q) = %q, want %q", c.input, got, c.want)
		}
	}
}

// --- render ---

func TestRenderRoundTrip(t *testing.T) {
	m := Memory{
		Name:        "test-fact",
		Description: "A test fact",
		Type:        TypeUser,
		Body:        "The body of the fact.",
	}
	rendered := render(m, "test-fact")
	fm, body := splitFrontmatter(rendered)
	if fm["name"] != "test-fact" {
		t.Errorf("name = %q", fm["name"])
	}
	if fm["description"] != "A test fact" {
		t.Errorf("description = %q", fm["description"])
	}
	if fm["type"] != "user" {
		t.Errorf("type = %q", fm["type"])
	}
	if !strings.Contains(body, "The body of the fact.") {
		t.Errorf("body = %q", body)
	}
}

func TestRenderNormalizesType(t *testing.T) {
	m := Memory{Name: "x", Description: "d", Type: Type("unknown"), Body: "b"}
	rendered := render(m, "x")
	fm, _ := splitFrontmatter(rendered)
	if fm["type"] != "project" {
		t.Errorf("unknown type should normalize to project, got %q", fm["type"])
	}
}

// --- loadMemory ---

func TestLoadMemoryNoFrontmatter(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "no-fm.md")
	os.WriteFile(f, []byte("just a body\nno frontmatter"), 0o644)
	m, ok := loadMemory(f)
	if !ok {
		t.Fatal("loadMemory should succeed for files without frontmatter")
	}
	// Name should be derived from filename.
	if m.Name != "no-fm" {
		t.Errorf("name = %q, want no-fm", m.Name)
	}
	if !strings.Contains(m.Body, "just a body") {
		t.Errorf("body = %q", m.Body)
	}
}

func TestLoadMemoryMissingFile(t *testing.T) {
	_, ok := loadMemory("/nonexistent/path.md")
	if ok {
		t.Error("loadMemory should return false for missing file")
	}
}

func TestLoadMemoryEmptyFile(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "empty.md")
	os.WriteFile(f, nil, 0o644)
	m, ok := loadMemory(f)
	if !ok {
		t.Fatal("loadMemory should succeed for empty files")
	}
	if m.Name != "empty" {
		t.Errorf("name = %q", m.Name)
	}
}

// --- Store.List edge cases ---

func TestListSkipsNonMdFiles(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "fact.md"), []byte("---\nname: fact\n---\nbody"), 0o644)
	os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("not a memory"), 0o644)
	os.WriteFile(filepath.Join(dir, "MEMORY.md"), []byte("# Memory\n"), 0o644)
	s := Store{Dir: dir}
	list := s.List()
	if len(list) != 1 {
		t.Fatalf("want 1 memory, got %d", len(list))
	}
	if list[0].Name != "fact" {
		t.Errorf("name = %q", list[0].Name)
	}
}

func TestListEmptyDir(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	if list := s.List(); len(list) != 0 {
		t.Errorf("empty dir should return empty list, got %d", len(list))
	}
}

func TestListSortedByName(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"zebra", "alpha", "middle"} {
		os.WriteFile(filepath.Join(dir, name+".md"), []byte("---\nname: "+name+"\n---\nbody"), 0o644)
	}
	s := Store{Dir: dir}
	list := s.List()
	if len(list) != 3 {
		t.Fatalf("want 3, got %d", len(list))
	}
	if list[0].Name != "alpha" || list[1].Name != "middle" || list[2].Name != "zebra" {
		t.Errorf("not sorted: %v %v %v", list[0].Name, list[1].Name, list[2].Name)
	}
}

// --- Store.Save edge cases ---

func TestSaveEmptyName(t *testing.T) {
	s := Store{Dir: t.TempDir()}
	_, err := s.Save(Memory{Name: "", Description: "d", Body: "b"})
	if err == nil {
		t.Fatal("expected error for empty name")
	}
}

func TestSaveCreatesDir(t *testing.T) {
	dir := t.TempDir()
	s := Store{Dir: filepath.Join(dir, "deep", "nested", "memory")}
	_, err := s.Save(Memory{Name: "test", Description: "d", Body: "b"})
	if err != nil {
		t.Fatalf("Save should create dirs: %v", err)
	}
	if _, err := os.Stat(filepath.Join(s.Dir, "test.md")); err != nil {
		t.Fatal("memory file should exist")
	}
}

// --- Store.Path ---

func TestStorePath(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "memory")
	s := Store{Dir: dir}
	got := s.Path("My Fact")
	if want := filepath.Join(dir, "my-fact.md"); got != want {
		t.Errorf("Path = %q, want %q", got, want)
	}
}
