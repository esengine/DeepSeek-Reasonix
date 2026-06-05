package command

import (
	"os"
	"path/filepath"
	"testing"
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

func names(cmds []Command) []string {
	out := make([]string, len(cmds))
	for i, c := range cmds {
		out[i] = c.Name
	}
	return out
}
