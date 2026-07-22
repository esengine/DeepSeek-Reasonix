package cli

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/provider"
)

func TestRunAppliesHomeBeforeCommandRouting(t *testing.T) {
	base := t.TempDir()
	home := filepath.Join(base, "profile")
	t.Setenv("REASONIX_HOME", filepath.Join(base, "old-home"))
	t.Setenv("REASONIX_STATE_HOME", filepath.Join(base, "old-state"))
	t.Setenv("REASONIX_CACHE_HOME", filepath.Join(base, "old-cache"))

	if code := Run([]string{"version", "--home", home}, "test"); code != 0 {
		t.Fatalf("Run exit code = %d", code)
	}
	if got := os.Getenv("REASONIX_HOME"); got != home {
		t.Fatalf("REASONIX_HOME = %q, want %q", got, home)
	}
	if got := os.Getenv("REASONIX_STATE_HOME"); got != home {
		t.Fatalf("REASONIX_STATE_HOME = %q, want %q", got, home)
	}
	if got := os.Getenv("REASONIX_CACHE_HOME"); got != filepath.Join(home, "cache") {
		t.Fatalf("REASONIX_CACHE_HOME = %q", got)
	}
}

func TestExtractGlobalHome(t *testing.T) {
	cases := []struct {
		args     []string
		wantArgs []string
		wantHome string
	}{
		{[]string{"--home", "profiles/a", "version"}, []string{"version"}, "profiles/a"},
		{[]string{"version", "--home", "profiles/a"}, []string{"version"}, "profiles/a"},
		{[]string{"--home=profile with spaces", "-p", "task"}, []string{"-p", "task"}, "profile with spaces"},
		{[]string{"run", "--", "--home", "literal"}, []string{"run", "--", "--home", "literal"}, ""},
	}
	for _, tc := range cases {
		gotArgs, gotHome, err := extractGlobalHome(tc.args)
		if err != nil {
			t.Fatalf("extractGlobalHome(%#v): %v", tc.args, err)
		}
		if !reflect.DeepEqual(gotArgs, tc.wantArgs) || gotHome != tc.wantHome {
			t.Fatalf("extractGlobalHome(%#v) = (%#v, %q), want (%#v, %q)", tc.args, gotArgs, gotHome, tc.wantArgs, tc.wantHome)
		}
	}
}

func TestExtractGlobalHomeRejectsMissingValue(t *testing.T) {
	for _, args := range [][]string{{"--home"}, {"--home="}, {"--home", "--"}} {
		if _, _, err := extractGlobalHome(args); err == nil {
			t.Fatalf("extractGlobalHome(%#v) unexpectedly succeeded", args)
		}
	}
}

func TestSplitAllowedToolRules(t *testing.T) {
	got, err := splitAllowedToolRules([]string{
		"Bash(git *) Edit,read_file",
		"Bash(go test ./...) Edit(docs/**)",
		"Edit",
	})
	if err != nil {
		t.Fatalf("splitAllowedToolRules: %v", err)
	}
	want := []string{"Bash(git *)", "Edit", "read_file", "Bash(go test ./...)", "Edit(docs/**)"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("rules = %#v, want %#v", got, want)
	}
}

func TestSplitAllowedToolRulesRejectsUnbalancedParentheses(t *testing.T) {
	for _, input := range []string{"Bash(git *", "Bash(git *))"} {
		if _, err := splitAllowedToolRules([]string{input}); err == nil {
			t.Fatalf("splitAllowedToolRules(%q) unexpectedly succeeded", input)
		}
	}
}

func TestNormalizeOptionalResumeArg(t *testing.T) {
	got := normalizeOptionalResumeArg([]string{"--model", "x", "--resume", "session-id", "--copy"})
	want := []string{"--model", "x", "--resume=session-id", "--copy"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("normalized args = %#v, want %#v", got, want)
	}
	got = normalizeOptionalResumeArg([]string{"-r", "--copy"})
	if !reflect.DeepEqual(got, []string{"-r", "--copy"}) {
		t.Fatalf("bare resume args = %#v", got)
	}
}

func TestHasLeadingPrintFlag(t *testing.T) {
	cases := []struct {
		args []string
		want bool
	}{
		{[]string{"-p", "task"}, true},
		{[]string{"--print", "task"}, true},
		{[]string{"--model", "x", "-p", "task"}, true},
		{[]string{"--effort", "max", "--print"}, true},
		{[]string{"--model", "x", "task"}, false},
		{[]string{"--", "-p"}, false}, // after -- it is a literal prompt token
		{[]string{"--model", "x", "--", "-p"}, false},
	}
	for _, tc := range cases {
		if got := hasLeadingPrintFlag(tc.args); got != tc.want {
			t.Fatalf("hasLeadingPrintFlag(%#v) = %v, want %v", tc.args, got, tc.want)
		}
	}
}

func TestStripLeadingPrintFlag(t *testing.T) {
	cases := []struct {
		args []string
		want []string
	}{
		{[]string{"-p", "task"}, []string{"task"}},
		{[]string{"--model", "x", "-p", "task"}, []string{"--model", "x", "task"}},
		{[]string{"--print", "--model", "x"}, []string{"--model", "x"}},
		// Only the first print token is dropped; a later "--print" after "--" is prompt text.
		{[]string{"-p", "--", "--print"}, []string{"--", "--print"}},
		{[]string{"--model", "x", "task"}, []string{"--model", "x", "task"}},
	}
	for _, tc := range cases {
		if got := stripLeadingPrintFlag(tc.args); !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("stripLeadingPrintFlag(%#v) = %#v, want %#v", tc.args, got, tc.want)
		}
	}
}

func TestResolveSessionQueryByIDAndPreview(t *testing.T) {
	dir := t.TempDir()
	first := saveQueryTestSession(t, dir, "alpha-session.jsonl", "fix provider configuration")
	_ = saveQueryTestSession(t, dir, "beta-session.jsonl", "improve terminal picker")

	got, err := resolveSessionQuery(dir, "alpha-session")
	if err != nil || got != first {
		t.Fatalf("resolve by ID = (%q, %v), want %q", got, err, first)
	}
	got, err = resolveSessionQuery(dir, "provider configuration")
	if err != nil || got != first {
		t.Fatalf("resolve by preview = (%q, %v), want %q", got, err, first)
	}
	if _, err := resolveSessionQuery(dir, "session"); err == nil || !strings.Contains(err.Error(), "ambiguous") {
		t.Fatalf("ambiguous query error = %v", err)
	}
	if _, err := resolveSessionQuery(dir, "missing"); err == nil || !strings.Contains(err.Error(), "no session") {
		t.Fatalf("missing query error = %v", err)
	}
}

func saveQueryTestSession(t *testing.T, dir, name, prompt string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	session := agent.NewSession("")
	session.Add(provider.Message{Role: provider.RoleUser, Content: prompt})
	session.Add(provider.Message{Role: provider.RoleAssistant, Content: "done"})
	if err := session.Save(path); err != nil {
		t.Fatal(err)
	}
	return path
}
