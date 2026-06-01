package skill

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- IsValidName ---

func TestIsValidName(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{"valid-name", true},
		{"CamelCase", true},
		{"with.dot", true},
		{"with_underscore", true},
		{"a", true},
		{"A123", true},
		{"", false},
		{"-starts-dash", false},
		{"has space", false},
		{"has/slash", false},
		{strings.Repeat("a", 65), false}, // too long
		{strings.Repeat("a", 64), true},  // max length
	}
	for _, c := range cases {
		if got := IsValidName(c.name); got != c.want {
			t.Errorf("IsValidName(%q) = %v, want %v", c.name, got, c.want)
		}
	}
}

// --- splitFrontmatter ---

func TestSplitFrontmatterNoFence(t *testing.T) {
	fm, body := splitFrontmatter("just body")
	if len(fm) != 0 {
		t.Errorf("expected empty fm, got %v", fm)
	}
	if body != "just body" {
		t.Errorf("body = %q", body)
	}
}

func TestSplitFrontmatterUnclosed(t *testing.T) {
	fm, body := splitFrontmatter("---\nkey: val\n\nno closing")
	if len(fm) != 0 {
		t.Errorf("unclosed fence should return empty fm, got %v", fm)
	}
	if !strings.Contains(body, "---") {
		t.Errorf("body should contain original: %q", body)
	}
}

func TestSplitFrontmatterEmpty(t *testing.T) {
	fm, body := splitFrontmatter("")
	if len(fm) != 0 {
		t.Errorf("empty input should return empty fm, got %v", fm)
	}
	if body != "" {
		t.Errorf("body = %q", body)
	}
}

func TestSplitFrontmatterQuotedValues(t *testing.T) {
	fm, _ := splitFrontmatter("---\ndescription: \"quoted\"\n---\n")
	if fm["description"] != "quoted" {
		t.Errorf("description = %q", fm["description"])
	}
}

// --- parseAllowedTools ---

func TestParseAllowedToolsEmpty(t *testing.T) {
	if got := parseAllowedTools(""); got != nil {
		t.Errorf("empty = %v, want nil", got)
	}
	if got := parseAllowedTools("   "); got != nil {
		t.Errorf("whitespace = %v, want nil", got)
	}
}

func TestParseAllowedToolsSingle(t *testing.T) {
	got := parseAllowedTools("bash")
	if len(got) != 1 || got[0] != "bash" {
		t.Errorf("single = %v", got)
	}
}

func TestParseAllowedToolsMultiple(t *testing.T) {
	got := parseAllowedTools("read_file, grep, bash")
	if len(got) != 3 {
		t.Errorf("count = %d, want 3", len(got))
	}
	if got[0] != "read_file" || got[1] != "grep" || got[2] != "bash" {
		t.Errorf("tools = %v", got)
	}
}

func TestParseAllowedToolsTrailingComma(t *testing.T) {
	got := parseAllowedTools("bash,")
	if len(got) != 1 || got[0] != "bash" {
		t.Errorf("trailing comma = %v", got)
	}
}

func TestParseAllowedToolsExtraSpaces(t *testing.T) {
	got := parseAllowedTools("  bash  ,  grep  ")
	if len(got) != 2 || got[0] != "bash" || got[1] != "grep" {
		t.Errorf("extra spaces = %v", got)
	}
}

// --- parseRunAs ---

func TestParseRunAsExplicit(t *testing.T) {
	if parseRunAs("subagent", "", "") != RunSubagent {
		t.Error("explicit subagent should return RunSubagent")
	}
	if parseRunAs("inline", "", "") != RunInline {
		t.Error("explicit inline should return RunInline")
	}
}

func TestParseRunAsContextFork(t *testing.T) {
	if parseRunAs("", "fork", "") != RunSubagent {
		t.Error("context: fork should return RunSubagent")
	}
	if parseRunAs("", "FORK", "") != RunSubagent {
		t.Error("context: FORK should return RunSubagent")
	}
}

func TestParseRunAsAgent(t *testing.T) {
	if parseRunAs("", "", "some-agent") != RunSubagent {
		t.Error("non-empty agent should return RunSubagent")
	}
}

func TestParseRunAsDefault(t *testing.T) {
	if parseRunAs("", "", "") != RunInline {
		t.Error("all empty should default to RunInline")
	}
	if parseRunAs("unknown", "", "") != RunInline {
		t.Error("unknown runAs should default to RunInline")
	}
}

// --- resolveCustomPaths ---

func TestResolveCustomPathsTilde(t *testing.T) {
	home := t.TempDir()
	got := resolveCustomPaths([]string{"~/skills"}, "/base", home)
	if len(got) != 1 || got[0] != filepath.Join(home, "skills") {
		t.Errorf("tilde expansion = %v", got)
	}
}

func TestResolveCustomPathsRelative(t *testing.T) {
	base := t.TempDir()
	got := resolveCustomPaths([]string{"./my-skills"}, base, "/home")
	if len(got) != 1 || got[0] != filepath.Join(base, "my-skills") {
		t.Errorf("relative = %v", got)
	}
}

func TestResolveCustomPathsAbsolute(t *testing.T) {
	abs := filepath.Join(t.TempDir(), "absolute", "path")
	got := resolveCustomPaths([]string{abs}, "/base", "/home")
	if len(got) != 1 || got[0] != abs {
		t.Errorf("absolute = %v", got)
	}
}

func TestResolveCustomPathsEmpty(t *testing.T) {
	got := resolveCustomPaths([]string{"", "  "}, "/base", "/home")
	if len(got) != 0 {
		t.Errorf("empty paths should be filtered, got %v", got)
	}
}

// --- dedupePaths ---

func TestDedupePaths(t *testing.T) {
	got := dedupePaths([]string{"/a", "/b", "/a", "/c", "/b"})
	if len(got) != 3 || got[0] != "/a" || got[1] != "/b" || got[2] != "/c" {
		t.Errorf("deduped = %v", got)
	}
}

func TestDedupePathsEmpty(t *testing.T) {
	got := dedupePaths(nil)
	if len(got) != 0 {
		t.Errorf("nil = %v", got)
	}
}

// --- stubBody ---

func TestStubBody(t *testing.T) {
	body := stubBody("my-skill")
	if !strings.Contains(body, "name: my-skill") {
		t.Error("stub should contain the skill name")
	}
	if !strings.Contains(body, "description:") {
		t.Error("stub should contain description field")
	}
	if !strings.Contains(body, "# my-skill") {
		t.Error("stub should contain the skill name as heading")
	}
}

// --- Read edge cases ---

func TestReadInvalidName(t *testing.T) {
	home := t.TempDir()
	st := New(Options{HomeDir: home, DisableBuiltins: true})
	_, ok := st.Read("invalid name!")
	if ok {
		t.Error("invalid name should return ok=false")
	}
}

func TestReadNotFound(t *testing.T) {
	home := t.TempDir()
	st := New(Options{HomeDir: home, DisableBuiltins: true})
	_, ok := st.Read("nonexistent")
	if ok {
		t.Error("nonexistent skill should return ok=false")
	}
}

// --- Create edge cases ---

func TestCreateInvalidName(t *testing.T) {
	home := t.TempDir()
	st := New(Options{HomeDir: home, DisableBuiltins: true})
	_, err := st.Create("invalid name!", ScopeGlobal)
	if err == nil {
		t.Error("invalid name should error")
	}
}

func TestCreateProjectScopeRequiresRoot(t *testing.T) {
	home := t.TempDir()
	st := New(Options{HomeDir: home, DisableBuiltins: true})
	_, err := st.Create("test", ScopeProject)
	if err == nil {
		t.Error("project scope without root should error")
	}
}

func TestCreateDirectoryLayoutSkill(t *testing.T) {
	home := t.TempDir()
	skillsRoot := filepath.Join(home, ".reasonix", "skills", "existing", "SKILL.md")
	os.MkdirAll(filepath.Dir(skillsRoot), 0o755)
	os.WriteFile(skillsRoot, []byte("---\ndescription: exists\n---\nbody"), 0o644)
	st := New(Options{HomeDir: home, DisableBuiltins: true})
	_, err := st.Create("existing", ScopeGlobal)
	if err == nil {
		t.Error("should refuse to overwrite directory-layout skill")
	}
}

// --- New edge cases ---

func TestNewWithCustomPaths(t *testing.T) {
	custom := t.TempDir()
	st := New(Options{HomeDir: t.TempDir(), CustomPaths: []string{custom}, DisableBuiltins: true})
	roots := st.Roots()
	found := false
	for _, r := range roots {
		if r.Dir == custom && r.Scope == ScopeCustom {
			found = true
			break
		}
	}
	if !found {
		t.Error("custom path not in roots")
	}
}

func TestHasProjectScope(t *testing.T) {
	st1 := New(Options{HomeDir: t.TempDir(), ProjectRoot: "/some/project"})
	if !st1.HasProjectScope() {
		t.Error("with project root should return true")
	}
	st2 := New(Options{HomeDir: t.TempDir()})
	if st2.HasProjectScope() {
		t.Error("without project root should return false")
	}
}
