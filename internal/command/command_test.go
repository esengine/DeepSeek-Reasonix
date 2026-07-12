package command

import (
	"os"
	"path/filepath"
	"testing"

	fileencoding "reasonix/internal/fileutil/encoding"
)

func TestRender(t *testing.T) {
	c := Command{Body: "Review $1 focusing on $ARGUMENTS. Cost: $$5. Missing: [$3]"}
	got := c.Render([]string{"main.go", "bugs"})
	want := "Review main.go focusing on main.go bugs. Cost: $5. Missing: []"
	if got != want {
		t.Errorf("Render = %q, want %q", got, want)
	}

	// No args: $ARGUMENTS and $N collapse to empty.
	if got := (Command{Body: "x=$ARGUMENTS y=$1"}).Render(nil); got != "x= y=" {
		t.Errorf("empty-args Render = %q", got)
	}
}

func write(t *testing.T, dir, rel, content string) {
	t.Helper()
	p := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestLoad(t *testing.T) {
	dir := t.TempDir()
	write(t, dir, "review.md", "---\ndescription: Review the diff\nargument-hint: [area]\n---\nReview, focus on $ARGUMENTS.")
	write(t, dir, "plain.md", "No frontmatter, just $1.")
	write(t, dir, "git/commit.md", "---\ndescription: Commit\n---\nWrite a commit message.")
	write(t, dir, "notes.txt", "ignored — not markdown")

	cmds, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cmds) != 3 {
		t.Fatalf("loaded %d commands, want 3 (%v)", len(cmds), names(cmds))
	}

	byName := map[string]Command{}
	for _, c := range cmds {
		byName[c.Name] = c
	}

	r, ok := byName["review"]
	if !ok || r.Description != "Review the diff" || r.ArgHint != "[area]" {
		t.Errorf("review parsed wrong: %+v", r)
	}
	if r.Body != "Review, focus on $ARGUMENTS." {
		t.Errorf("review body = %q", r.Body)
	}
	if p := byName["plain"]; p.Body != "No frontmatter, just $1." || p.Description != "" {
		t.Errorf("plain parsed wrong: %+v", p)
	}
	if _, ok := byName["git:commit"]; !ok {
		t.Errorf("subdir namespacing failed: %v", names(cmds))
	}
}

func TestLoadDecodesGB18030CommandFile(t *testing.T) {
	dir := t.TempDir()
	body := "---\ndescription: 中文命令\nargument-hint: [主题]\n---\n请总结 $ARGUMENTS。"
	path := filepath.Join(dir, "summary.md")
	if err := os.WriteFile(path, fileencoding.Encode(body, fileencoding.GB18030), 0o644); err != nil {
		t.Fatal(err)
	}

	cmds, err := Load(dir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cmds) != 1 || cmds[0].Description != "中文命令" || cmds[0].ArgHint != "[主题]" || cmds[0].Body != "请总结 $ARGUMENTS。" {
		t.Fatalf("decoded command = %+v", cmds)
	}
}

func TestLoadOverrideAndMissingDir(t *testing.T) {
	user := t.TempDir()
	project := t.TempDir()
	write(t, user, "review.md", "USER version")
	write(t, project, "review.md", "PROJECT version")

	// Later dir (project) wins on a name clash; a non-existent dir is skipped.
	cmds, err := Load("/no/such/dir", user, project)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(cmds) != 1 || cmds[0].Body != "PROJECT version" {
		t.Errorf("override failed: %+v", cmds)
	}
}

func TestLoadRootsUsesCanonicalPluginNamesAndHiddenCompatibleShortName(t *testing.T) {
	pluginDir := t.TempDir()
	projectDir := t.TempDir()
	write(t, pluginDir, "plan.md", "---\ndescription: Plugin plan\n---\nPLUGIN $ARGUMENTS")
	write(t, pluginDir, "status.md", "PLUGIN STATUS")
	write(t, projectDir, "plan.md", "---\ndescription: Project plan\n---\nPROJECT $ARGUMENTS")

	cmds, err := LoadRoots(
		Root{Path: pluginDir, Plugin: "planning-with-files"},
		Root{Path: projectDir},
	)
	if err != nil {
		t.Fatalf("LoadRoots: %v", err)
	}
	byName := map[string]Command{}
	for _, cmd := range cmds {
		byName[cmd.Name] = cmd
	}
	if got := byName["plan"]; got.Body != "PROJECT $ARGUMENTS" || got.Plugin != "" || got.ShortName != "" || got.Hidden {
		t.Fatalf("short-name winner = %+v, want project command", got)
	}
	canonical, ok := byName["planning-with-files:plan"]
	if !ok || canonical.Body != "PLUGIN $ARGUMENTS" || canonical.Plugin != "planning-with-files" || canonical.ShortName != "plan" || canonical.Hidden {
		t.Fatalf("canonical plugin command = %+v, %v", canonical, ok)
	}
	if got := byName["status"]; got.Plugin != "planning-with-files" || got.ShortName != "status" || !got.Hidden {
		t.Fatalf("short compatibility command = %+v, want hidden plugin alias", got)
	}
	if got := byName["planning-with-files:status"]; got.Plugin != "planning-with-files" || got.ShortName != "status" || got.Hidden {
		t.Fatalf("canonical status command = %+v", got)
	}
	if got := canonical.Render([]string{"feature"}); got != "PLUGIN feature" {
		t.Fatalf("canonical command render = %q", got)
	}
}

func TestLoadRootsDoesNotReplaceAnExplicitQualifiedCommand(t *testing.T) {
	pluginDir := t.TempDir()
	projectDir := t.TempDir()
	write(t, pluginDir, "plan.md", "PLUGIN")
	write(t, projectDir, "plan.md", "PROJECT")
	write(t, projectDir, "planning-with-files/plan.md", "EXPLICIT QUALIFIED")

	cmds, err := LoadRoots(
		Root{Path: pluginDir, Plugin: "planning-with-files"},
		Root{Path: projectDir},
	)
	if err != nil {
		t.Fatalf("LoadRoots: %v", err)
	}
	for _, cmd := range cmds {
		if cmd.Name == "planning-with-files:plan" {
			if cmd.Body != "EXPLICIT QUALIFIED" || cmd.Plugin != "" || cmd.ShortName != "" || cmd.Hidden {
				t.Fatalf("explicit qualified command was replaced: %+v", cmd)
			}
			return
		}
	}
	t.Fatal("explicit qualified command missing")
}

func TestLoadRootsOmitsAmbiguousShortPluginName(t *testing.T) {
	alpha := t.TempDir()
	beta := t.TempDir()
	write(t, alpha, "plan.md", "ALPHA")
	write(t, beta, "plan.md", "BETA")
	cmds, err := LoadRoots(Root{Path: alpha, Plugin: "alpha"}, Root{Path: beta, Plugin: "beta"})
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]Command{}
	for _, cmd := range cmds {
		byName[cmd.Name] = cmd
	}
	if _, ok := byName["plan"]; ok {
		t.Fatal("ambiguous plugin short name must not remain invocable")
	}
	if byName["alpha:plan"].Body != "ALPHA" || byName["beta:plan"].Body != "BETA" {
		t.Fatalf("canonical plugin commands = %+v", cmds)
	}
}

func names(cmds []Command) []string {
	out := make([]string, len(cmds))
	for i, c := range cmds {
		out[i] = c.Name
	}
	return out
}
