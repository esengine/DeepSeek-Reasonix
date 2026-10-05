package boot

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"reasonix/internal/contract/config"
	"reasonix/internal/contract/observe"
	"reasonix/internal/runtime/agent/testutil"
)

const (
	outsideMark = "OUTSIDE-SECRET-BODY"
	insideMark  = "INSIDE-BODY"
	refusedText = "outside the folders this run may read"
)

// scopeWorld is a workspace with a neighbour it must not read through.
type scopeWorld struct {
	root, other, home string
}

func newScopeWorld(t *testing.T) scopeWorld {
	t.Helper()
	root := observeProject(t)
	w := scopeWorld{root: root, other: robustTempDir(t), home: os.Getenv("HOME")}
	writeFile(t, w.other, "data.go", "package other\n// "+outsideMark+"\nfunc Secret() {}\n")
	writeFile(t, w.other, ".env", "TOKEN="+outsideMark+"\n")
	writeFile(t, w.home, "home-note.txt", outsideMark)
	writeFile(t, root, "inside.go", "package in\n// "+insideMark+"\nfunc Fine() {}\n")
	return w
}

func (w scopeWorld) link(t *testing.T, target, name string) {
	t.Helper()
	if err := os.Symlink(target, filepath.Join(w.root, name)); err != nil {
		t.Skipf("symlinks are not available here: %v", err)
	}
}

// run puts each call to the read-only posture in its own round and returns
// what the model was told for it.
func (w scopeWorld) run(t *testing.T, calls map[string][2]string, order []string) map[string]string {
	t.Helper()
	var turns []testutil.Turn
	for _, id := range order {
		turns = append(turns, call(id, calls[id][0], calls[id][1]))
	}
	prov := testutil.NewMock("observe", append(turns, testutil.Turn{Text: "done"})...)
	ctrl, _ := buildObserved(t, w.root, prov, observe.RunContext{ScheduleID: "s", TriggerID: "t"})
	if err := ctrl.Run(context.Background(), "read"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	return toolResults(prov.Requests())
}

func mustRefuse(t *testing.T, results map[string]string, ids ...string) {
	t.Helper()
	for _, id := range ids {
		got := results[id]
		if strings.Contains(got, outsideMark) {
			t.Errorf("%s leaked what is outside the workspace: %q", id, got)
		}
		if !strings.Contains(got, refusedText) {
			t.Errorf("%s was not refused for its scope: %q", id, got)
		}
	}
}

func jsonPath(tool, key, path string) [2]string {
	return [2]string{tool, fmt.Sprintf(`{%q:%q}`, key, filepath.ToSlash(path))}
}

// Every read tool, given a path that leaves the workspace by any route.
func TestEffectObserveReadsStayInsideTheWorkspace(t *testing.T) {
	w := newScopeWorld(t)
	w.link(t, filepath.Join(w.other, "data.go"), "file-link.go")
	w.link(t, w.other, "dir-link")
	w.link(t, filepath.Join(w.root, "inside.go"), "inner-link.go")
	up := "../" + filepath.Base(w.other) + "/data.go"
	calls := map[string][2]string{
		"read-abs":    jsonPath("read_file", "path", filepath.Join(w.other, "data.go")),
		"read-dotdot": jsonPath("read_file", "path", up),
		"read-link":   jsonPath("read_file", "path", "file-link.go"),
		"read-dir":    jsonPath("read_file", "path", "dir-link/data.go"),
		"read-env":    jsonPath("read_file", "path", filepath.Join(w.other, ".env")),
		"read-home":   jsonPath("read_file", "path", filepath.Join(w.home, "home-note.txt")),
		"ls-abs":      jsonPath("ls", "path", w.other),
		"ls-home":     jsonPath("ls", "path", w.home),
		"ls-link":     jsonPath("ls", "path", "dir-link"),
		"glob-abs":    jsonPath("glob", "pattern", filepath.Join(w.other, "*.go")),
		"glob-rec":    jsonPath("glob", "pattern", filepath.Join(w.other, "**", "*.go")),
		"glob-link":   jsonPath("glob", "pattern", "dir-link/*.go"),
		"grep-abs":    {"grep", fmt.Sprintf(`{"pattern":"Secret","path":%q}`, filepath.ToSlash(w.other))},
		"grep-link":   {"grep", `{"pattern":"Secret","path":"dir-link"}`},
		"grep-file":   {"grep", `{"pattern":"Secret","path":"file-link.go"}`},
		"idx-abs":     {"code_index", fmt.Sprintf(`{"action":"outline","path":%q}`, filepath.ToSlash(w.other))},
		"idx-link":    {"code_index", `{"action":"outline","path":"dir-link"}`},
		"idx-file":    {"code_index", `{"action":"outline","path":"file-link.go"}`},
		"ok-read":     jsonPath("read_file", "path", "inside.go"),
		"ok-link":     jsonPath("read_file", "path", "inner-link.go"),
		"ok-ls":       jsonPath("ls", "path", "."),
		"ok-grep":     {"grep", `{"pattern":"Fine"}`},
		"ok-glob":     jsonPath("glob", "pattern", "*.go"),
		"walk-grep":   {"grep", `{"pattern":"Secret","path":"."}`},
		"walk-glob":   jsonPath("glob", "pattern", "**/*.go"),
		"walk-ls":     {"ls", `{"path":".","recursive":true}`},
		"walk-idx":    {"code_index", `{"action":"outline","path":"."}`},
	}
	order := make([]string, 0, len(calls))
	for id := range calls {
		order = append(order, id)
	}
	results := w.run(t, calls, order)

	mustRefuse(t, results, "read-abs", "read-dotdot", "read-link", "read-dir", "read-env", "read-home",
		"ls-abs", "ls-home", "ls-link", "glob-abs", "glob-rec", "glob-link",
		"grep-abs", "grep-link", "grep-file", "idx-abs", "idx-link", "idx-file")
	for _, id := range []string{"ok-read", "ok-link"} {
		if !strings.Contains(results[id], insideMark) {
			t.Errorf("%s: a path that stays inside the workspace was refused: %q", id, results[id])
		}
	}
	for _, id := range []string{"ok-ls", "ok-grep", "ok-glob"} {
		if !strings.Contains(results[id], "inside.go") && !strings.Contains(results[id], "Fine") {
			t.Errorf("%s: the workspace itself was not readable: %q", id, results[id])
		}
	}
	if strings.Contains(results["ok-glob"], "file-link.go") || strings.Contains(results["ok-glob"], "dir-link") {
		t.Errorf("a glob listed a link that leads outside the workspace: %q", results["ok-glob"])
	}
	// Walking the workspace must not step through a link to what lies outside it.
	for _, id := range []string{"walk-grep", "walk-glob", "walk-ls", "walk-idx"} {
		if strings.Contains(results[id], outsideMark) || strings.Contains(results[id], "Secret") {
			t.Errorf("%s walked through a link out of the workspace: %q", id, results[id])
		}
	}
}

// The [secrets] switches are the user's to turn off for their own sessions; the
// posture turns them back on.
// On POSIX a `..` after a link names the link target's parent, so a path that
// looks like it stays in the workspace can end in the folder beside it.
func TestEffectObserveDotDotAfterALinkStaysInside(t *testing.T) {
	w := newScopeWorld(t)
	sub := filepath.Join(w.other, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, w.other, "notes.txt", outsideMark)
	w.link(t, sub, "sublink")
	via := filepath.ToSlash(w.root) + "/sublink/.."
	calls := map[string][2]string{
		"read": {"read_file", fmt.Sprintf(`{"path":%q}`, via+"/notes.txt")},
		"ls":   {"ls", fmt.Sprintf(`{"path":%q}`, via)},
		"grep": {"grep", fmt.Sprintf(`{"pattern":"OUTSIDE","path":%q}`, via)},
		"glob": {"glob", fmt.Sprintf(`{"pattern":%q}`, via+"/*")},
		"idx":  {"code_index", fmt.Sprintf(`{"action":"outline","path":%q}`, via)},
	}
	results := w.run(t, calls, []string{"read", "ls", "grep", "glob", "idx"})
	for id, got := range results {
		if strings.Contains(got, outsideMark) || (runtime.GOOS != "windows" && !strings.Contains(got, refusedText) && (strings.Contains(got, "notes.txt") || strings.Contains(got, "data.go"))) {
			t.Errorf("%s reached the folder beside the workspace: %q", id, got)
		}
	}
	if runtime.GOOS != "windows" {
		mustRefuse(t, results, "read", "ls", "grep", "glob", "idx")
	}
}

func TestEffectObserveKeepsSecretProtectionOn(t *testing.T) {
	w := newScopeWorld(t)
	writeUserConfig(t, userModel+"\n[secrets]\nprotect_sensitive_files = false\nprotect_credential_files = false\n")
	writeFile(t, w.root, ".env", "TOKEN="+outsideMark+"\n")
	results := w.run(t, map[string][2]string{"env": jsonPath("read_file", "path", ".env")}, []string{"env"})
	if strings.Contains(results["env"], outsideMark) {
		t.Fatalf("a sensitive file inside the workspace was readable: %q", results["env"])
	}
}

// A case variant of an outside path reaches the same bytes on these filesystems.
func TestEffectObserveCaseVariantsStayOutside(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "windows" {
		t.Skip("the filesystem is case-sensitive here")
	}
	w := newScopeWorld(t)
	variant := filepath.Join(strings.ToUpper(w.other), "DATA.GO")
	results := w.run(t, map[string][2]string{"upper": jsonPath("read_file", "path", variant)}, []string{"upper"})
	if strings.Contains(results["upper"], outsideMark) {
		t.Fatalf("a case variant read what is outside: %q", results["upper"])
	}
}

// A junction is the Windows symlink an unprivileged user can make.
func TestEffectObserveJunctionOutOfTheWorkspaceIsRefused(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("junctions exist only on windows")
	}
	w := newScopeWorld(t)
	if out, err := exec.Command("cmd", "/c", "mklink", "/J", filepath.Join(w.root, "junction"), w.other).CombinedOutput(); err != nil {
		t.Skipf("mklink /J failed: %v %s", err, out)
	}
	results := w.run(t, map[string][2]string{
		"read": jsonPath("read_file", "path", "junction/data.go"),
		"ls":   jsonPath("ls", "path", "junction"),
		"grep": {"grep", `{"pattern":"Secret","path":"junction"}`},
		"walk": {"grep", `{"pattern":"Secret","path":"."}`},
	}, []string{"read", "ls", "grep", "walk"})
	mustRefuse(t, results, "read", "ls", "grep")
	if strings.Contains(results["walk"], outsideMark) {
		t.Fatalf("a walk went through a junction: %q", results["walk"])
	}
}

func TestBuildRefusesAWorkspaceContainingHomeOrRoot(t *testing.T) {
	root := observeProject(t)
	setBootTokenProfileTestProvider(t, testutil.NewMock("observe", testutil.Turn{Text: "ok"}))
	for name, dir := range map[string]string{"home": os.Getenv("HOME"), "parent of home": filepath.Dir(os.Getenv("HOME")), "filesystem root": string(filepath.Separator)} {
		if runtime.GOOS == "windows" && name == "filesystem root" {
			dir = filepath.VolumeName(root) + `\`
		}
		if err := checkObserveRoot(dir); !errors.Is(err, ErrObserveRootTooBroad) {
			t.Errorf("%s: checkObserveRoot(%q) = %v, want ErrObserveRootTooBroad", name, dir, err)
		}
	}
	if err := checkObserveRoot(root); err != nil {
		t.Errorf("an ordinary workspace was refused: %v", err)
	}
	t.Setenv("HOME", "")
	t.Setenv("USERPROFILE", "")
	if err := checkObserveRoot(root); !errors.Is(err, ErrObserveRootTooBroad) {
		t.Errorf("with no home directory known, checkObserveRoot = %v, want a refusal", err)
	}
	_ = config.Roots{}
}
