package control

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/testenv"
)

func writeAt(t *testing.T, root, rel string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func inserts(items []SlashItem) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.Insert
	}
	return out
}

func hasInsert(items []SlashItem, insert string) bool {
	for _, it := range items {
		if it.Insert == insert {
			return true
		}
	}
	return false
}

// A reference completed mid-sentence must replace the whole token, not the part
// before the caret — otherwise accepting leaves the old suffix behind.
func TestCompleteRefReplacesWholeTokenAroundCaret(t *testing.T) {
	root := testenv.TempDir(t)
	writeAt(t, root, "notes.md")
	line := "see @no and more"
	caret := strings.Index(line, " and")

	got := Complete(line, caret, CompletionData{WorkspaceRoot: root})
	if got.Kind != CompleteRef {
		t.Fatalf("kind = %q, want %q", got.Kind, CompleteRef)
	}
	if line[got.From:got.To] != "@no" {
		t.Fatalf("span %q, want %q", line[got.From:got.To], "@no")
	}
	if !hasInsert(got.Items, "@notes.md") {
		t.Fatalf("items = %v, want @notes.md", inserts(got.Items))
	}

	caretInside := strings.Index(line, "o and") // "@n|o"
	inside := Complete(line, caretInside, CompletionData{WorkspaceRoot: root})
	if line[inside.From:inside.To] != "@no" {
		t.Fatalf("mid-token span %q, want the full token %q", line[inside.From:inside.To], "@no")
	}
}

// An email address is not a reference: '@' only opens the menu at a word start.
func TestCompleteIgnoresInfixAt(t *testing.T) {
	root := testenv.TempDir(t)
	writeAt(t, root, "notes.md")
	if got := Complete("mail me@example.com", 19, CompletionData{WorkspaceRoot: root}); got.Kind != "" {
		t.Fatalf("kind = %q, want no menu (items %v)", got.Kind, inserts(got.Items))
	}
}

// Scoped completion is what a client reached over HTTP gets: it must not be
// offered a path outside the workspace, because the turn would refuse it.
func TestCompleteScopedStaysInsideWorkspace(t *testing.T) {
	parent := testenv.TempDir(t)
	root := filepath.Join(parent, "work")
	writeAt(t, root, "inside.md")
	writeAt(t, parent, "outside.md")

	inside := Complete("@ins", 4, CompletionData{WorkspaceRoot: root, Scoped: true})
	if !hasInsert(inside.Items, "@inside.md") {
		t.Fatalf("scoped completion should list workspace files, got %v", inserts(inside.Items))
	}

	for _, token := range []string{"@../", "@" + parent + "/"} {
		got := Complete(token, len(token), CompletionData{WorkspaceRoot: root, Scoped: true})
		if len(got.Items) != 0 {
			t.Fatalf("scoped completion of %q leaked %v", token, inserts(got.Items))
		}
		open := Complete(token, len(token), CompletionData{WorkspaceRoot: root})
		if !hasInsert(open.Items, "@"+strings.TrimPrefix(token, "@")+"outside.md") && !hasInsert(open.Items, "@../outside.md") {
			t.Fatalf("unscoped completion of %q should still reach the parent, got %v", token, inserts(open.Items))
		}
	}
}

// A directory descends and a name with spaces survives the whitespace-delimited
// token grammar, because the insert carries the escaping the parser reverses.
func TestCompleteRefEscapesAndDescends(t *testing.T) {
	root := testenv.TempDir(t)
	writeAt(t, root, "my dir/inner.md")

	got := Complete("@my", 3, CompletionData{WorkspaceRoot: root})
	if !hasInsert(got.Items, `@my\ dir/`) {
		t.Fatalf("items = %v, want an escaped directory insert", inserts(got.Items))
	}
	for _, it := range got.Items {
		if it.Insert == `@my\ dir/` && !it.Descend {
			t.Fatal("a directory must be a descend, not a completed reference")
		}
	}
	deeper := Complete(`@my\ dir/`, 9, CompletionData{WorkspaceRoot: root})
	if !hasInsert(deeper.Items, `@my\ dir/inner.md`) {
		t.Fatalf("descending an escaped directory listed %v", inserts(deeper.Items))
	}
}

// The slash menu replaces the whole command word and matches as a subsequence,
// so a half-remembered name still finds its command.
func TestCompleteSlashNames(t *testing.T) {
	names := []SlashItem{
		{Label: "/compact", Insert: "/compact "},
		{Label: "/context", Insert: "/context"},
		{Label: "/review", Insert: "/review "},
	}
	got := Complete("/ctx", 4, CompletionData{Names: names})
	if got.Kind != CompleteSlash || got.From != 0 || got.To != 4 {
		t.Fatalf("got %+v, want the whole word replaced", got)
	}
	if !hasInsert(got.Items, "/context") {
		t.Fatalf("items = %v, want the subsequence match /context", inserts(got.Items))
	}
	if hasInsert(got.Items, "/review ") {
		t.Fatalf("items = %v, want no unrelated command", inserts(got.Items))
	}
}

// Past the command word the menu completes arguments, replacing only the token
// under the caret so the command itself survives.
func TestCompleteSlashArgument(t *testing.T) {
	line := "/skills dis"
	got := Complete(line, len(line), CompletionData{})
	if got.Kind != CompleteSlashArg {
		t.Fatalf("kind = %q, want %q", got.Kind, CompleteSlashArg)
	}
	if line[got.From:got.To] != "dis" {
		t.Fatalf("span %q, want %q", line[got.From:got.To], "dis")
	}
	if !hasInsert(got.Items, "disable ") {
		t.Fatalf("items = %v, want disable", inserts(got.Items))
	}
}

// A reference under the caret wins over the slash menu, so "/review @f" still
// completes the file rather than re-offering commands.
func TestCompleteRefInsideSlashArguments(t *testing.T) {
	root := testenv.TempDir(t)
	writeAt(t, root, "notes.md")
	line := "/review @no"
	got := Complete(line, len(line), CompletionData{WorkspaceRoot: root})
	if got.Kind != CompleteRef || !hasInsert(got.Items, "@notes.md") {
		t.Fatalf("got kind %q items %v, want the file menu", got.Kind, inserts(got.Items))
	}
}

// Nothing to complete answers with an empty list rather than a nil one: this
// crosses a JSON boundary where null would be a second empty value.
func TestCompleteEmptyIsNotNil(t *testing.T) {
	if got := Complete("just prose", 10, CompletionData{}); got.Items == nil {
		t.Fatal("items must be an empty slice, not nil")
	}
}

// A token typed out in full must not keep a row that would replace it with
// itself: it reads as a duplicate, and it swallows the Enter meant to send.
func TestCompleteDropsTheRowThatChangesNothing(t *testing.T) {
	root := testenv.TempDir(t)
	writeAt(t, root, "notes.md")
	writeAt(t, root, "notes.md.bak")

	got := Complete("@notes.md", 9, CompletionData{WorkspaceRoot: root})
	if hasInsert(got.Items, "@notes.md") {
		t.Fatalf("items = %v, want the already-typed reference gone", inserts(got.Items))
	}
	if !hasInsert(got.Items, "@notes.md.bak") {
		t.Fatalf("items = %v, want the genuine alternative kept", inserts(got.Items))
	}

	// The last one standing leaves no menu at all, so Enter belongs to the composer.
	only := Complete("@notes.md.bak", 13, CompletionData{WorkspaceRoot: root})
	if len(only.Items) != 0 {
		t.Fatalf("items = %v, want no menu once nothing can change", inserts(only.Items))
	}
}

// The menu says what it filtered on, so a frontend can point at the reason a
// row is there instead of showing an unexplained fuzzy hit.
func TestCompleteReportsTheQueryItFilteredOn(t *testing.T) {
	root := testenv.TempDir(t)
	writeAt(t, root, "internal/serve/complete.go")

	ref := Complete("@internal/serve/comp", 20, CompletionData{WorkspaceRoot: root})
	if ref.Query != "comp" {
		t.Fatalf("query = %q, want the segment being typed", ref.Query)
	}
	slash := Complete("/ctx", 4, CompletionData{Names: []SlashItem{{Label: "/context", Insert: "/context"}}})
	if slash.Query != "/ctx" {
		t.Fatalf("query = %q, want the typed command word", slash.Query)
	}
}

// A reference names something the repository keeps. Offering what .gitignore
// excludes puts a path in front of the user that the tree does not track and the
// turn has no reason to read — node_modules being the one every JavaScript
// project carries, and the reason this is read from the file rather than from a
// list of names somebody guessed.
func TestRefDirItemsSkipsIgnoredEntries(t *testing.T) {
	root := t.TempDir()
	mustMkdir(t, filepath.Join(root, ".git"))
	mustWrite(t, filepath.Join(root, ".gitignore"), "node_modules/\ndist/\n")
	for _, d := range []string{"node_modules", "dist", "internal"} {
		mustMkdir(t, filepath.Join(root, d))
	}
	mustWrite(t, filepath.Join(root, "go.mod"), "module x\n")

	labels := map[string]bool{}
	for _, it := range RefDirItems(root, "", true) {
		labels[it.Label] = true
	}
	for _, want := range []string{"internal/", "go.mod"} {
		if !labels[want] {
			t.Errorf("%q missing from the listing: %v", want, labels)
		}
	}
	for _, unwanted := range []string{"node_modules/", "dist/"} {
		if labels[unwanted] {
			t.Errorf("%q is ignored by the repository and was still offered", unwanted)
		}
	}
}

// Filtering the listing must not lock the directory away: a user who types the
// path has said what they mean, the way grep searches a path named explicitly.
func TestRefDirItemsStillDescendsIntoAnIgnoredDirectory(t *testing.T) {
	root := t.TempDir()
	mustMkdir(t, filepath.Join(root, ".git"))
	mustWrite(t, filepath.Join(root, ".gitignore"), "node_modules/\n")
	mustMkdir(t, filepath.Join(root, "node_modules", "left-pad"))
	mustWrite(t, filepath.Join(root, "node_modules", "left-pad", "index.js"), "")

	items := RefDirItems(root, "node_modules/left-pad/", true)
	found := false
	for _, it := range items {
		if it.Label == "index.js" {
			found = true
		}
	}
	if !found {
		t.Fatalf("typing the path into an ignored directory completed nothing: %+v", items)
	}
}

func mustMkdir(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
}

func mustWrite(t *testing.T, p, body string) {
	t.Helper()
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// A bare @token is typed from the name the user remembers, not its spelling:
// the letters in order are enough to reach a file anywhere in the workspace.
func TestCompleteRefFindsFileBySubsequence(t *testing.T) {
	root := testenv.TempDir(t)
	writeAt(t, root, "src/pages/project-info/tabs/ProjectInfoNew.tsx")

	got := Complete("@proinf", 7, CompletionData{WorkspaceRoot: root})
	if !hasInsert(got.Items, "@src/pages/project-info/tabs/ProjectInfoNew.tsx") {
		t.Fatalf("items = %v, want the ProjectInfoNew.tsx reference", inserts(got.Items))
	}
}
