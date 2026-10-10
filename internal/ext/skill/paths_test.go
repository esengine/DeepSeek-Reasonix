package skill

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"

	"reasonix/internal/base/testenv"
)

func TestParsePathsFrontmatter(t *testing.T) {
	cases := []struct {
		name, raw      string
		valid, invalid []string
	}{
		{"empty", "", nil, nil},
		{"single", "*.go", []string{"*.go"}, nil},
		{"csv", "*.js, *.ts", []string{"*.js", "*.ts"}, nil},
		{"bracketed and quoted", `["src/**/*.ts", 'docs/']`, []string{"src/**/*.ts", "docs/"}, nil},
		{"braces keep their commas", "*.{ts,tsx}, lib/**", []string{"*.{ts,tsx}", "lib/**"}, nil},
		{"brace budget is shared by the list", manyBraces(9) + ", " + manyBraces(8) + ", " + manyBraces(1), []string{manyBraces(9), manyBraces(8), manyBraces(1)}, nil},
		{"budget overflow lands in invalid", manyBraces(9) + ", " + manyBraces(9)[:len(manyBraces(9))-4] + "y.go", []string{manyBraces(9)}, []string{manyBraces(9)[:len(manyBraces(9))-4] + "y.go"}},
		{"too many brace groups rejected", manyBraces(10), nil, []string{manyBraces(10)}},
		{"oversized glob rejected", strings.Repeat("a", maxPathGlobLen+1), nil, []string{strings.Repeat("a", maxPathGlobLen+1)}},
		{"duplicates collapse", "*.go, *.go", []string{"*.go"}, nil},
		{"negation rejected", "!vendor/**, *.go", []string{"*.go"}, []string{"!vendor/**"}},
		{"parent traversal rejected", "../x/*.go, a/../b", nil, []string{"../x/*.go", "a/../b"}},
		{"malformed rejected", "src/[a", nil, []string{"src/[a"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			valid, invalid := parsePathsFrontmatter(tc.raw)
			if !reflect.DeepEqual(valid, tc.valid) || !reflect.DeepEqual(invalid, tc.invalid) {
				t.Fatalf("parse(%q) = %q / %q, want %q / %q", tc.raw, valid, invalid, tc.valid, tc.invalid)
			}
		})
	}
}

func TestPathsFrontmatterReadsFromSkillFiles(t *testing.T) {
	home := testenv.TempDir(t)
	writeSkill(t, home, ".claude/skills/csv/SKILL.md", "---\nname: csv\ndescription: d\npaths: \"*.js, *.ts\"\n---\nB")
	writeSkill(t, home, ".claude/skills/list/SKILL.md", "---\nname: list\ndescription: d\npaths:\n  - \"src/**/*.go\"\n  - docs/\n---\nB")
	writeSkill(t, home, ".claude/skills/plain/SKILL.md", "---\nname: plain\ndescription: d\n---\nB")
	writeSkill(t, home, ".claude/skills/onlypaths/SKILL.md", "---\npaths: \"*.rs\"\n---\nB")
	store := New(Options{HomeDir: home, DisableBuiltins: true})

	csv, _ := store.Read("csv")
	if !reflect.DeepEqual(csv.Paths, []string{"*.js", "*.ts"}) {
		t.Fatalf("csv paths = %q", csv.Paths)
	}
	list, _ := store.Read("list")
	if !reflect.DeepEqual(list.Paths, []string{"src/**/*.go", "docs/"}) {
		t.Fatalf("list paths = %q", list.Paths)
	}
	plain, _ := store.Read("plain")
	if plain.PathGated() || plain.Paths != nil || plain.InvalidPaths != nil {
		t.Fatalf("a skill without paths must stay ungated: %+v", plain)
	}
}

func TestEligibleMatching(t *testing.T) {
	root := testenv.TempDir(t)
	cases := []struct {
		name  string
		globs []string
		seen  []string
		want  bool
	}{
		{"ungated needs no hit", nil, nil, true},
		{"gated with no hit", []string{"*.go"}, nil, false},
		{"slashless glob is anchored at the root", []string{"*.go"}, []string{"a/b/c.go"}, false},
		{"double star glob matches at any depth", []string{"**/*.go"}, []string{"a/b/c.go"}, true},
		{"slashless glob matches at root", []string{"*.go"}, []string{"c.go"}, true},
		{"slashless glob misses other extension", []string{"*.go"}, []string{"c.ts"}, false},
		{"slashless dir glob is root only", []string{"*.md"}, []string{"docs/readme.md"}, false},
		{"slashless name is root only", []string{"Makefile"}, []string{"sub/Makefile"}, false},
		{"slashless name at root", []string{"Makefile"}, []string{"Makefile"}, true},
		{"anchored glob", []string{"src/**/*.ts"}, []string{"src/a/b.ts"}, true},
		{"anchored glob is not any depth", []string{"src/*.ts"}, []string{"x/src/a.ts"}, false},
		{"double star matches directly under prefix", []string{"src/**/*.ts"}, []string{"src/a.ts"}, true},
		{"leading double star matches at root", []string{"**/*.ts"}, []string{"a.ts"}, true},
		{"trailing slash means the directory tree", []string{"docs/"}, []string{"docs/a/b.md"}, true},
		{"trailing slash does not match a sibling", []string{"docs/"}, []string{"docsx/a.md"}, false},
		{"dot slash prefix", []string{"./src/*.go"}, []string{"src/a.go"}, true},
		{"leading slash anchors to the root", []string{"/src/*.go"}, []string{"src/a.go"}, true},
		{"any of several globs", []string{"*.rs", "*.go"}, []string{"x.go"}, true},
		{"braces", []string{"**/*.{ts,tsx}"}, []string{"a/b.tsx"}, true},
		{"nested braces", []string{"{src,lib/{a,b}}/*.go"}, []string{"lib/b/x.go"}, true},
		{"case sensitive", []string{"*.GO"}, []string{"a.go"}, false},
		{"negation never matches", []string{"!*.go"}, []string{"a.go"}, false},
		{"valid glob still works beside a rejected one", []string{"*.go", "!x"}, []string{"a.go"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := NewPathHits(root)
			for _, p := range tc.seen {
				if err := h.Observe(p); err != nil {
					t.Fatal(err)
				}
			}
			valid, invalid := parsePathsFrontmatter(joinGlobs(tc.globs))
			sk := Skill{Name: "s", Paths: valid, InvalidPaths: invalid}
			if got := h.Eligible(sk); got != tc.want {
				t.Fatalf("Eligible(%q after %q) = %v, want %v", tc.globs, tc.seen, got, tc.want)
			}
		})
	}
}

func TestEligibleSkillWithOnlyRejectedGlobsNeverActivates(t *testing.T) {
	h := NewPathHits(testenv.TempDir(t))
	_ = h.Observe("a.go")
	sk := Skill{Name: "s", InvalidPaths: []string{"!a.go"}}
	if !sk.PathGated() || h.Eligible(sk) {
		t.Fatal("a paths declaration that parsed to nothing must hide the skill, not free it")
	}
}

func TestEligibleSurvivesWhatTheHostObservedAfterwards(t *testing.T) {
	h := NewPathHits(testenv.TempDir(t))
	sk := Skill{Name: "s", Paths: []string{"*.go"}}
	if h.Eligible(sk) {
		t.Fatal("eligible before any hit")
	}
	_ = h.Observe("a.go")
	_ = h.Observe("b.txt")
	if !h.Eligible(sk) {
		t.Fatal("not eligible after a matching hit")
	}
}

func TestObservePathForms(t *testing.T) {
	root := testenv.TempDir(t)
	h := NewPathHits(root)
	for _, p := range []string{
		"src/a.go",
		"./src/a.go",
		"src/../src/a.go",
		filepath.Join(root, "src", "a.go"),
		"src/new_file_not_created_yet.go",
		"",
		"  ",
		".",
		root,
	} {
		if err := h.Observe(p); err != nil {
			t.Fatalf("Observe(%q) = %v", p, err)
		}
	}
	want := []string{"src/a.go", "src/new_file_not_created_yet.go"}
	if got := h.Seen(); !reflect.DeepEqual(got, want) {
		t.Fatalf("Seen = %q, want %q", got, want)
	}
}

func TestObserveRefusesPathsOutsideTheWorkspace(t *testing.T) {
	base := testenv.TempDir(t)
	root := filepath.Join(base, "ws")
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatal(err)
	}
	h := NewPathHits(root)
	for _, p := range []string{"../x.go", "a/../../x.go", filepath.Join(base, "x.go"), filepath.Join(base, "ws-sibling", "x.go")} {
		if err := h.Observe(p); !errors.Is(err, ErrPathOutsideWorkspace) {
			t.Fatalf("Observe(%q) = %v, want ErrPathOutsideWorkspace", p, err)
		}
	}
	if got := h.Seen(); len(got) != 0 {
		t.Fatalf("a refused path was recorded: %q", got)
	}
}

func TestObserveSymlinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("creating symlinks needs privileges on Windows")
	}
	base := testenv.TempDir(t)
	root := filepath.Join(base, "ws")
	outside := filepath.Join(base, "outside")
	for _, d := range []string{filepath.Join(root, "real"), outside} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite := func(p string) {
		t.Helper()
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	mustWrite(filepath.Join(root, "real", "a.go"))
	mustWrite(filepath.Join(outside, "secret.go"))
	for link, target := range map[string]string{
		filepath.Join(root, "in"):       filepath.Join(root, "real"),
		filepath.Join(root, "escape"):   outside,
		filepath.Join(root, "file.go"):  filepath.Join(root, "real", "a.go"),
		filepath.Join(root, "leak.go"):  filepath.Join(outside, "secret.go"),
		filepath.Join(root, "dangling"): filepath.Join(outside, "missing"),
		filepath.Join(root, "loop"):     filepath.Join(root, "loop"),
	} {
		if err := os.Symlink(target, link); err != nil {
			t.Fatal(err)
		}
	}
	h := NewPathHits(root)

	for _, p := range []string{"in/a.go", "file.go"} {
		if err := h.Observe(p); err != nil {
			t.Fatalf("Observe(%q) inside the workspace = %v", p, err)
		}
	}
	if got, want := h.Seen(), []string{"file.go", "in/a.go"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Seen = %q, want the names the files were reached by %q", got, want)
	}
	for _, p := range []string{"escape/secret.go", "leak.go", "escape/not_created_yet.go", "dangling", "loop"} {
		if err := h.Observe(p); !errors.Is(err, ErrPathOutsideWorkspace) {
			t.Fatalf("Observe(%q) through a link out of the workspace = %v, want ErrPathOutsideWorkspace", p, err)
		}
	}
}

func TestObserveAcceptsTheWorkspaceRootSpelledThroughItsSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("creating symlinks needs privileges on Windows")
	}
	base := testenv.TempDir(t)
	real := filepath.Join(base, "real")
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(base, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	h := NewPathHits(link)
	if err := h.Observe(filepath.Join(real, "a.go")); err != nil {
		t.Fatalf("real spelling of the root: %v", err)
	}
	if err := h.Observe(filepath.Join(link, "b.go")); err != nil {
		t.Fatalf("link spelling of the root: %v", err)
	}
	if got, want := h.Seen(), []string{"a.go", "b.go"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Seen = %q, want %q", got, want)
	}
}

func TestSlashFromNormalisesWindowsSeparators(t *testing.T) {
	if got := slashFrom(`src\pkg\a.go`, '\\'); got != "src/pkg/a.go" {
		t.Fatalf("windows separators: %q", got)
	}
	if got := slashFrom(`we\ird/a.go`, '/'); got != `we\ird/a.go` {
		t.Fatalf("a backslash is an ordinary name character where the separator is a slash: %q", got)
	}
}

func TestObserveConcurrent(t *testing.T) {
	h := NewPathHits(testenv.TempDir(t))
	var wg sync.WaitGroup
	for i := range 32 {
		wg.Go(func() {
			_ = h.Observe(fmt.Sprintf("d/f%d.go", i%8))
			_ = h.Eligible(Skill{Paths: []string{"*.go"}})
		})
	}
	wg.Wait()
	if got := len(h.Seen()); got != 8 {
		t.Fatalf("seen %d distinct paths, want 8", got)
	}
}

func joinGlobs(globs []string) string {
	return strings.Join(globs, ", ")
}

func manyBraces(groups int) string {
	return strings.Repeat("{a,a}/", groups) + "x.go"
}

func TestBraceExpansionsCountsWhatAGlobStandsFor(t *testing.T) {
	cases := map[string]int{
		"*.go":                1,
		"*.{ts,tsx}":          2,
		"{a,b}/{c,d}/*.{x,y}": 8,
		"{a,b{c,d}}":          3,
		`\{a,b\}`:             1,
		"stray}brace":         1,
		"{a,a}/{a,a}/{a,a}/x": 8,
		manyBraces(14):        maxPathExpansions + 1,
		manyBraces(18):        maxPathExpansions + 1,
		manyBraces(22):        maxPathExpansions + 1,
	}
	for glob, want := range cases {
		if got := braceExpansions(glob); got != want {
			t.Errorf("braceExpansions(%q) = %d, want %d", glob, got, want)
		}
	}
}

func TestExpansionHeavyGlobsNeverReachTheMatcher(t *testing.T) {
	h := NewPathHits(testenv.TempDir(t))
	_ = h.Observe(strings.Repeat("a/", 22) + "x.go")
	for _, groups := range []int{14, 18, 22} {
		raw := manyBraces(groups)
		valid, invalid := parsePathsFrontmatter(raw)
		if len(valid) != 0 || len(invalid) != 1 {
			t.Fatalf("%d groups parsed to %q / %q", groups, valid, invalid)
		}
		if _, ok := effectivePathPattern(raw); ok {
			t.Fatalf("%d groups reached the matcher", groups)
		}
		sk := Skill{Name: "s", Paths: []string{raw}, InvalidPaths: nil}
		if h.Eligible(sk) {
			t.Fatalf("%d groups made a skill eligible", groups)
		}
	}
	valid, _ := parsePathsFrontmatter(manyBraces(9))
	if len(valid) != 1 {
		t.Fatal("512 expansions is inside the budget and must be accepted")
	}
}

func TestEligibleDoesNotBlockObserve(t *testing.T) {
	h := NewPathHits(testenv.TempDir(t))
	for i := range 200 {
		_ = h.Observe(fmt.Sprintf("d%d/f.go", i))
	}
	sk := Skill{Name: "s", Paths: []string{"nomatch/**/*.rs", "**/*.zig"}}
	var wg sync.WaitGroup
	for i := range 16 {
		wg.Go(func() { _ = h.Eligible(sk) })
		wg.Go(func() { _ = h.Observe(fmt.Sprintf("n%d/f.go", i)) })
	}
	wg.Wait()
	if got := len(h.Seen()); got != 216 {
		t.Fatalf("seen %d, want 216", got)
	}
}

func TestObserveIgnoresDirectoriesAndResetForgets(t *testing.T) {
	root := testenv.TempDir(t)
	if err := os.MkdirAll(filepath.Join(root, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	h := NewPathHits(root)
	for _, p := range []string{"src", filepath.Join(root, "src")} {
		if err := h.Observe(p); err != nil {
			t.Fatalf("Observe(%q) = %v", p, err)
		}
	}
	if got := h.Seen(); len(got) != 0 {
		t.Fatalf("a directory was recorded as a touched file: %q", got)
	}
	_ = h.Observe("src/a.go")
	h.Reset()
	if got := h.Seen(); len(got) != 0 {
		t.Fatalf("Reset kept %q", got)
	}
}

func TestVerdictsAreMemoisedUntilTheSetChanges(t *testing.T) {
	h := NewPathHits(testenv.TempDir(t))
	sk := Skill{Name: "s", Paths: []string{"src/*.go"}}
	if first := h.Eligible(sk); first || h.Eligible(sk) {
		t.Fatal("eligible before any hit")
	}
	if got := len(h.verdicts.byKey); got != 1 {
		t.Fatalf("two identical questions left %d cached verdicts, want 1", got)
	}
	_ = h.Observe("docs/x.md")
	if got := len(h.verdicts.byKey); got != 0 {
		t.Fatalf("a new path left %d stale verdicts", got)
	}
	if h.Eligible(sk) {
		t.Fatal("an unrelated path made the skill eligible")
	}
	_ = h.Observe("docs/x.md")
	if got := len(h.verdicts.byKey); got != 1 {
		t.Fatalf("seeing the same path again dropped the verdict: %d cached", got)
	}
	_ = h.Observe("src/a.go")
	if !h.Eligible(sk) {
		t.Fatal("a matching path did not make the skill eligible")
	}
	h.Reset()
	if h.Eligible(sk) {
		t.Fatal("a reset set kept a verdict")
	}
}

func TestVisibleKeepsUngatedAndMatchedSkills(t *testing.T) {
	h := NewPathHits(testenv.TempDir(t))
	skills := []Skill{
		{Name: "plain"},
		{Name: "go", Paths: []string{"**/*.go"}},
		{Name: "md", Paths: []string{"*.md"}},
		{Name: "broken", InvalidPaths: []string{"!x"}},
	}
	names := func(in []Skill) []string {
		var out []string
		for _, s := range in {
			out = append(out, s.Name)
		}
		return out
	}
	if got, want := names(h.Visible(skills)), []string{"plain"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("before any hit: %q, want %q", got, want)
	}
	_ = h.Observe("src/a.go")
	if got, want := names(h.Visible(skills)), []string{"plain", "go"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("after a go file: %q, want %q", got, want)
	}
	var none *PathHits
	if got := none.Visible(skills); len(got) != len(skills) {
		t.Fatal("a nil set must leave every skill listed")
	}
}

func TestStoreOwnsOneSetAndAvailableNamesHonoursIt(t *testing.T) {
	home := testenv.TempDir(t)
	root := testenv.TempDir(t)
	writeSkill(t, root, ".reasonix/skills/plain/SKILL.md", "---\nname: plain\ndescription: d\n---\nB")
	writeSkill(t, root, ".reasonix/skills/gated/SKILL.md", "---\nname: gated\ndescription: d\npaths: \"src/*.go\"\n---\nB")
	store := New(Options{HomeDir: home, ProjectRoot: root, DisableBuiltins: true})
	if store.PathHits() == nil {
		t.Fatal("the store must own one stable set")
	}
	if got := availableNames(store); got != "plain" {
		t.Fatalf("before a hit: %q", got)
	}
	_ = store.PathHits().Observe("src/a.go")
	if got := availableNames(store); got != "gated, plain" {
		t.Fatalf("after a hit: %q", got)
	}
	var none *Store
	if none.PathHits() != nil {
		t.Fatal("a nil store has no set")
	}
}

func TestAvailableNamesSaysWhyWhenGatingEmptiesTheList(t *testing.T) {
	root := testenv.TempDir(t)
	store := New(Options{HomeDir: testenv.TempDir(t), ProjectRoot: root, DisableBuiltins: true})
	if got := availableNames(store); got != "(none — no skills defined)" {
		t.Fatalf("no skills at all: %q", got)
	}
	writeSkill(t, root, ".reasonix/skills/gated/SKILL.md", "---\nname: gated\ndescription: d\npaths: \"src/*.go\"\n---\nB")
	got := availableNames(store)
	if got != "(none apply to the files touched so far)" {
		t.Fatalf("skills exist but none is eligible: %q", got)
	}
	_ = store.PathHits().Observe("src/a.go")
	if got := availableNames(store); got != "gated" {
		t.Fatalf("after the hit: %q", got)
	}
}

func TestObserveRefusesNetworkPathBeforeAnyLookup(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("network paths are a Windows reading of a path")
	}
	prev := lookupExisting
	lookups := 0
	lookupExisting = func(p string) string { lookups++; return prev(p) }
	t.Cleanup(func() { lookupExisting = prev })

	hits := NewPathHits(t.TempDir())
	for _, p := range []string{`\\evil\share\x`, `//evil/share/x`, `\\?\UNC\evil\share\x`, `\\127.0.0.1\C$\Windows\win.ini`} {
		if err := hits.Observe(p); !errors.Is(err, ErrPathOutsideWorkspace) {
			t.Errorf("Observe(%q) = %v, want ErrPathOutsideWorkspace", p, err)
		}
	}
	if lookups != 0 {
		t.Fatalf("%d lookups before the refusal, want 0", lookups)
	}
}
