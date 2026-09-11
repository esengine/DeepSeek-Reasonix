package cli

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/pflag"
	"reasonix/internal/agent"
	"reasonix/internal/provider"
)

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

func TestRegisterContinueFlagShorthandParses(t *testing.T) {
	cases := []struct {
		args []string
		want bool
	}{
		{[]string{"-c"}, true},
		{[]string{"-c=true"}, true},
		{[]string{"--continue"}, true},
		{[]string{"--continue=true"}, true},
		{[]string{}, false},
	}
	for _, tc := range cases {
		fs := pflag.NewFlagSet("reasonix", pflag.ContinueOnError)
		cont := registerContinueFlag(fs)
		if err := fs.Parse(tc.args); err != nil {
			t.Fatalf("Parse(%#v): %v", tc.args, err)
		}
		if *cont != tc.want {
			t.Fatalf("Parse(%#v) continue = %v, want %v", tc.args, *cont, tc.want)
		}
	}
}

// Regression guard: registering the shorthand with BoolVar instead of BoolP
// leaves "-c" unparseable ("unknown shorthand flag") while accidentally
// accepting "--c" as a long flag name (the pre-fix bug, #7156/#7171).
func TestRegisterContinueFlagRejectsAccidentalLongC(t *testing.T) {
	fs := pflag.NewFlagSet("reasonix", pflag.ContinueOnError)
	cont := registerContinueFlag(fs)
	if err := fs.Parse([]string{"--c"}); err == nil {
		t.Fatalf("Parse(--c) should fail: --c must not exist as a long flag name")
	}
	if *cont {
		t.Fatalf("--c unexpectedly set the continue flag")
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

func TestResolveSessionQueryByMachineSessionID(t *testing.T) {
	identityKey := installMachineTestIdentity(t)
	dir := t.TempDir()
	path := saveQueryTestSession(t, dir, "opaque-branch.jsonl", "resume by machine id")
	machineID := machineSessionIDWithKey(agent.BranchID(path), identityKey)
	if machineID == "" || !looksLikeMachineSessionID(machineID) {
		t.Fatalf("machine session id = %q", machineID)
	}

	got, err := resolveSessionQuery(dir, machineID)
	if err != nil || got != path {
		t.Fatalf("resolve by machine id = (%q, %v), want %q", got, err, path)
	}
	missing := "session_" + strings.Repeat("0", 32)
	if _, err := resolveSessionQuery(dir, missing); err == nil || !strings.Contains(err.Error(), "no session") {
		t.Fatalf("missing machine id error = %v", err)
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
