package personality

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadEmpty(t *testing.T) {
	p := Load()
	if !p.Empty() {
		t.Fatal("expected empty personality")
	}
}

func TestLoadSingleFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, FileNameIdentity), []byte("I am a test agent."), 0644); err != nil {
		t.Fatal(err)
	}
	p := Load(dir)
	if p.Empty() {
		t.Fatal("expected non-empty personality")
	}
	if !strings.Contains(p.Identity, "I am a test agent") {
		t.Fatalf("unexpected identity: %q", p.Identity)
	}
}

func TestLoadAllFiles(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, FileNameIdentity), []byte("Identity"), 0644)
	os.WriteFile(filepath.Join(dir, FileNameSoul), []byte("Soul"), 0644)
	os.WriteFile(filepath.Join(dir, FileNameUser), []byte("User"), 0644)

	p := Load(dir)
	if p.Identity != "Identity" || p.Soul != "Soul" || p.User != "User" {
		t.Fatalf("unexpected: %+v", p)
	}
}

func TestLoadOverride(t *testing.T) {
	dir1 := t.TempDir()
	dir2 := t.TempDir()
	os.WriteFile(filepath.Join(dir1, FileNameIdentity), []byte("Project identity"), 0644)
	os.WriteFile(filepath.Join(dir2, FileNameIdentity), []byte("Home identity"), 0644)
	os.WriteFile(filepath.Join(dir2, FileNameSoul), []byte("Home soul"), 0644)

	// First dir wins (project overrides home)
	p := Load(dir1, dir2)
	if p.Identity != "Project identity" {
		t.Fatalf("expected project identity, got %q", p.Identity)
	}
	if p.Soul != "Home soul" {
		t.Fatalf("expected home soul, got %q", p.Soul)
	}
}

func TestCompose(t *testing.T) {
	base := "You are an agent."
	p := Personality{
		Identity: "I am a coding assistant.",
		Soul:     "I am concise and helpful.",
		User:     "The user is a developer.",
	}
	result := Compose(base, p)
	if !strings.Contains(result, base) {
		t.Fatal("missing base prompt")
	}
	if !strings.Contains(result, "=== IDENTITY ===") {
		t.Fatal("missing identity section")
	}
	if !strings.Contains(result, "=== SOUL ===") {
		t.Fatal("missing soul section")
	}
	if !strings.Contains(result, "=== USER ===") {
		t.Fatal("missing user section")
	}
}

func TestComposeEmpty(t *testing.T) {
	base := "You are an agent."
	result := Compose(base, Personality{})
	if result != base {
		t.Fatalf("expected unchanged base, got %q", result)
	}
}

func TestProjectDirs(t *testing.T) {
	dirs := ProjectDirs("/tmp/project")
	found := false
	for _, d := range dirs {
		if strings.Contains(d, ".reasonix/personality") && strings.Contains(d, "/tmp/project") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected project .reasonix/personality in dirs: %v", dirs)
	}
}

func TestWriteAndReadFile(t *testing.T) {
	root := t.TempDir()
	path, err := WriteFile(FileNameIdentity, "Custom Identity", root)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(path, ".reasonix/personality/IDENTITY.md") {
		t.Fatalf("unexpected path: %s", path)
	}
	dirs := ProjectDirs(root)
	content, ok := ReadFile(FileNameIdentity, dirs)
	if !ok {
		t.Fatal("file not found after write")
	}
	if !strings.Contains(content, "Custom Identity") {
		t.Fatalf("unexpected content: %q", content)
	}
}

func TestDeleteFile(t *testing.T) {
	root := t.TempDir()
	_, err := WriteFile(FileNameIdentity, "To delete", root)
	if err != nil {
		t.Fatal(err)
	}
	if err := DeleteFile(FileNameIdentity, root); err != nil {
		t.Fatal(err)
	}
	dirs := ProjectDirs(root)
	if _, ok := ReadFile(FileNameIdentity, dirs); ok {
		t.Fatal("file should have been deleted")
	}
}

func TestList(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, FileNameIdentity), []byte("x"), 0644)
	os.WriteFile(filepath.Join(dir, FileNameSoul), []byte("x"), 0644)

	files := List([]string{dir})
	if len(files) != 2 {
		t.Fatalf("expected 2 files, got %v", files)
	}
}
