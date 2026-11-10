package installsource

import (
	"os"
	"path/filepath"
	"testing"

	"reasonix/internal/base/testenv"
	"reasonix/internal/ext/skilllock"
)

func sourceSkill(t *testing.T, root, body string) string {
	t.Helper()
	dir := filepath.Join(root, "my-skill")
	writeFile(t, filepath.Join(dir, "SKILL.md"), "---\nname: my-skill\ndescription: fixture\n---\n"+body)
	return dir
}

func TestACopyInstallRecordsItsSourceAndUninstallForgetsIt(t *testing.T) {
	home, project := testenv.TempDir(t), testenv.TempDir(t)
	src := sourceSkill(t, testenv.TempDir(t), "v1")
	tool := NewTool(Options{ProjectRoot: project, HomeDir: home})
	resp := execInstall(t, tool, map[string]any{"source": src, "kind": "skill", "mode": "copy", "scope": "global", "apply": true})
	if !resp.OK {
		t.Fatalf("install: %+v", resp)
	}
	reasonixHome := filepath.Join(home, ".reasonix")
	e, ok, err := skilllock.Find(reasonixHome, "my-skill", "global")
	if err != nil || !ok {
		t.Fatalf("no record after the install: %+v %v %v", e, ok, err)
	}
	if realSrc, _ := filepath.EvalSymlinks(src); e.Source != realSrc || e.Mode != "copy" || e.Digest == "" {
		t.Fatalf("record = %+v, want the folder it came from (%s)", e, realSrc)
	}
	installed := filepath.Join(reasonixHome, "skills", "my-skill")
	if got := skilllock.Assess(e, installed); got != skilllock.Synced {
		t.Fatalf("fresh install = %s", got)
	}
	writeFile(t, filepath.Join(src, "SKILL.md"), "---\nname: my-skill\ndescription: fixture\n---\nv2")
	if got := skilllock.Assess(e, installed); got != skilllock.SourceChanged {
		t.Fatalf("after the source moved = %s", got)
	}
	un := execInstall(t, tool, map[string]any{"op": "uninstall", "name": "my-skill", "scope": "global", "apply": true})
	if !un.OK {
		t.Fatalf("uninstall: %+v", un)
	}
	if _, ok, _ := skilllock.Find(reasonixHome, "my-skill", "global"); ok {
		t.Fatal("an uninstalled skill is still recorded")
	}
}

func TestAProjectInstallIsNotRecorded(t *testing.T) {
	home, project := testenv.TempDir(t), testenv.TempDir(t)
	src := sourceSkill(t, testenv.TempDir(t), "v1")
	tool := NewTool(Options{ProjectRoot: project, HomeDir: home})
	if resp := execInstall(t, tool, map[string]any{"source": src, "kind": "skill", "mode": "copy", "scope": "project", "apply": true}); !resp.OK {
		t.Fatalf("project install: %+v", resp)
	}
	if _, err := os.Stat(skilllock.Path(filepath.Join(home, ".reasonix"))); !os.IsNotExist(err) {
		t.Fatalf("a project skill wrote a lock: %v", err)
	}
}

// A source that is a git checkout, or holds an in-folder link, installs as its
// files only; the record must still read it as in step with the copy.
func TestACopyOfAGitCheckoutWithALinkReadsAsInStep(t *testing.T) {
	home, project := testenv.TempDir(t), testenv.TempDir(t)
	src := sourceSkill(t, testenv.TempDir(t), "v1")
	writeFile(t, filepath.Join(src, ".git", "HEAD"), "ref: refs/heads/main")
	writeFile(t, filepath.Join(src, "notes.txt"), "n")
	if os.Symlink("notes.txt", filepath.Join(src, "alias.txt")) != nil {
		t.Skip("no symlinks here")
	}
	tool := NewTool(Options{ProjectRoot: project, HomeDir: home})
	if resp := execInstall(t, tool, map[string]any{"source": src, "kind": "skill", "mode": "copy", "scope": "global", "apply": true}); !resp.OK {
		t.Fatalf("install: %+v", resp)
	}
	reasonixHome := filepath.Join(home, ".reasonix")
	e, ok, err := skilllock.Find(reasonixHome, "my-skill", "global")
	if err != nil || !ok {
		t.Fatalf("no record: %v %v", ok, err)
	}
	if got := skilllock.Assess(e, filepath.Join(reasonixHome, "skills", "my-skill")); got != skilllock.Synced {
		t.Fatalf("a fresh copy of a git checkout reads %s, want synced", got)
	}
}
