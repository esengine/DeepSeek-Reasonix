package sandbox

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestNormalizeWriteDirHomeAndRelative(t *testing.T) {
	home := t.TempDir()
	work := filepath.Join(home, "proj")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}
	abs, display, err := NormalizeWriteDir("~/.local", work, home)
	if err != nil {
		t.Fatal(err)
	}
	wantLocal, err := ResolveAbsPath(filepath.Join(home, ".local"))
	if err != nil {
		t.Fatal(err)
	}
	if abs != wantLocal {
		t.Fatalf("abs = %q, want %q", abs, wantLocal)
	}
	if display != "~/.local" {
		t.Fatalf("display = %q, want ~/.local", display)
	}
	abs, display, err = NormalizeWriteDir("${HOME}/.cache", work, home)
	if err != nil {
		t.Fatal(err)
	}
	wantCache, err := ResolveAbsPath(filepath.Join(home, ".cache"))
	if err != nil {
		t.Fatal(err)
	}
	if abs != wantCache || display != "~/.cache" {
		t.Fatalf("HOME expand abs=%q display=%q", abs, display)
	}
	abs, _, err = NormalizeWriteDir("out", work, home)
	if err != nil {
		t.Fatal(err)
	}
	wantOut, err := ResolveAbsPath(filepath.Join(work, "out"))
	if err != nil {
		t.Fatal(err)
	}
	if abs != wantOut {
		t.Fatalf("relative = %q, want %q", abs, wantOut)
	}
}

func TestNormalizeWriteDirRejectsGlob(t *testing.T) {
	if _, _, err := NormalizeWriteDir("~/.local/*", t.TempDir(), t.TempDir()); err == nil {
		t.Fatal("glob should be rejected")
	}
}

func TestNormalizeWriteDirResolvesSymlinkParent(t *testing.T) {
	root := t.TempDir()
	realDir := filepath.Join(root, "real")
	if err := os.Mkdir(realDir, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(realDir, link); err != nil {
		t.Fatal(err)
	}
	abs, _, err := NormalizeWriteDir(filepath.Join(link, "nested"), root, root)
	if err != nil {
		t.Fatal(err)
	}
	want, err := ResolveAbsPath(filepath.Join(realDir, "nested"))
	if err != nil {
		t.Fatal(err)
	}
	if abs != want {
		t.Fatalf("symlink parent = %q, want %q", abs, want)
	}
}

func TestCollapseWriteRootsDropsChildren(t *testing.T) {
	parent := filepath.Join(t.TempDir(), "a")
	child := filepath.Join(parent, "b")
	got := CollapseWriteRoots([]string{child, parent, parent})
	if len(got) != 1 || got[0] != parent {
		t.Fatalf("CollapseWriteRoots = %v, want [%s]", got, parent)
	}
}

func TestIsFilesystemRoot(t *testing.T) {
	if !IsFilesystemRoot(string(filepath.Separator)) {
		t.Fatal("separator root should be rejected")
	}
	if runtime.GOOS != "windows" && !IsFilesystemRoot("/") {
		t.Fatal("/ should be a filesystem root")
	}
	if IsFilesystemRoot(t.TempDir()) {
		t.Fatal("temp dir is not a filesystem root")
	}
	if runtime.GOOS == "windows" {
		if !IsFilesystemRoot(`C:\`) {
			t.Fatal(`C:\ should be a filesystem root`)
		}
	}
}

func TestIsHomeDir(t *testing.T) {
	home := t.TempDir()
	if !IsHomeDir(home, home) {
		t.Fatal("home should match itself")
	}
	if IsHomeDir(filepath.Join(home, "x"), home) {
		t.Fatal("child of home is not the home directory")
	}
}

func TestProtectedWritePath(t *testing.T) {
	state := t.TempDir()
	if !IsProtectedWritePath(state, state) {
		t.Fatal("state root must be protected")
	}
	if !IsProtectedWritePath(filepath.Join(state, "sessions", "a.json"), state) {
		t.Fatal("sessions store must be protected")
	}
	if !IsProtectedWritePath(filepath.Join(state, "projects", "slug", "sessions", "a.json"), state) {
		t.Fatal("project sessions must be protected")
	}
	if !IsProtectedWritePath(filepath.Join(state, "projects", "slug"), state) {
		t.Fatal("project state must not be dynamically writable")
	}
	if !IsProtectedWritePath(filepath.Join(state, "settings.json"), state) {
		t.Fatal("settings.json must be protected")
	}
	if IsProtectedWritePath(filepath.Join(state, "skills", "x.md"), state) {
		t.Fatal("ordinary state files are not protected")
	}
	if err := ValidateWriteDir(filepath.Join(state, "sessions"), state); err == nil {
		t.Fatal("requesting sessions should be rejected")
	}
	if err := ValidateWriteDir(string(filepath.Separator), state); err == nil {
		t.Fatal("filesystem root should be rejected")
	}
	if err := ValidateWriteDir(filepath.Join(state, "skills"), state); err != nil {
		t.Fatalf("skills dir should be allowed: %v", err)
	}
}

func TestProtectedWriteRootsProtectsFutureState(t *testing.T) {
	state := t.TempDir()
	want, err := ResolveAbsPath(state)
	if err != nil {
		t.Fatal(err)
	}
	got := ProtectedWriteRoots(state)
	if len(got) != 1 || got[0] != want {
		t.Fatalf("ProtectedWriteRoots = %v, want state boundary %q", got, want)
	}
	if !PathWithin(got[0], filepath.Join(want, "desktop-future.json")) {
		t.Fatal("state boundary should cover files created after sandbox startup")
	}
}

func TestFormatConfigWritePath(t *testing.T) {
	home := filepath.Join(string(filepath.Separator), "Users", "x")
	got := FormatConfigWritePath(filepath.Join(home, ".local"), home)
	if got != "${HOME}/.local" {
		t.Fatalf("FormatConfigWritePath = %q", got)
	}
}

func TestNormalizeWriteDirsCollapsesDisplay(t *testing.T) {
	home := t.TempDir()
	parent := filepath.Join(home, ".local")
	child := filepath.Join(parent, "bin")
	abs, display, broad, err := NormalizeWriteDirs([]string{child, parent}, home, home, filepath.Join(home, "state"))
	if err != nil {
		t.Fatal(err)
	}
	if broad {
		t.Fatal("child of home is not a home grant")
	}
	if len(abs) != 1 || len(display) != 1 || display[0] != "~/.local" {
		t.Fatalf("abs=%v display=%v", abs, display)
	}
}

func TestEnsureWriteDirCreatesAndKeepsExisting(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "a", "b")
	var err error
	dir, err = ResolveAbsPath(dir)
	if err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(t.TempDir(), "state")
	if got, err := EnsureWriteDir(dir, state); err != nil || got != dir {
		t.Fatal(err)
	}
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		t.Fatalf("created dir missing: %v", err)
	}
	if _, err := EnsureWriteDir(dir, state); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := EnsureWriteDir(file, state); err == nil {
		t.Fatal("file should not be accepted as a write directory")
	}
}

func TestEnsureWriteDirRejectsApprovalIdentityChange(t *testing.T) {
	root := t.TempDir()
	approvedParent := filepath.Join(root, "approved")
	if err := os.Mkdir(approvedParent, 0o755); err != nil {
		t.Fatal(err)
	}
	approved, _, err := NormalizeWriteDir(filepath.Join(approvedParent, "nested"), root, root)
	if err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(root, "state")
	projects := filepath.Join(state, "projects")
	if err := os.MkdirAll(projects, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(approvedParent); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(projects, approvedParent); err != nil {
		t.Fatal(err)
	}
	if _, err := EnsureWriteDir(approved, state); err == nil {
		t.Fatal("retargeting an approved ancestor into protected state must fail")
	}
}

func TestSameWritePathRequiresExactIdentity(t *testing.T) {
	approved := filepath.Join(t.TempDir(), "Approved")
	if !sameWritePath(approved, approved) {
		t.Fatal("identical approved paths should match")
	}
	if sameWritePath(approved, filepath.Join(filepath.Dir(approved), "approved")) {
		t.Fatal("case-only path changes must not reuse an existing approval")
	}
}
