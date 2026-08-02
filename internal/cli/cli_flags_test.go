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

func TestRegisterResumeFlagsShorthands(t *testing.T) {
	// Interactive registration: -c and -r are real shorthands, and a bare
	// --resume/-r (no value) opens the picker via NoOptDefVal.
	cases := []struct {
		args       []string
		wantCont   bool
		wantResume string
		wantCopy   bool
	}{
		{args: []string{"-c"}, wantCont: true},
		{args: []string{"-c=true"}, wantCont: true},
		{args: []string{"--continue"}, wantCont: true},
		{args: []string{"--continue=true"}, wantCont: true},
		{args: []string{"-r"}, wantResume: resumePickerSentinel},
		{args: []string{"--resume"}, wantResume: resumePickerSentinel},
		{args: []string{"-r", "provider-config"}, wantResume: "provider-config"},
		{args: []string{"-r=provider-config"}, wantResume: "provider-config"},
		{args: []string{"--resume", "provider-config"}, wantResume: "provider-config"},
		{args: []string{"--copy", "-c"}, wantCont: true, wantCopy: true},
	}
	for _, tc := range cases {
		fs := pflag.NewFlagSet("reasonix", pflag.ContinueOnError)
		fs.SetInterspersed(true)
		cont, resume, copySession := registerResumeFlags(fs, "resume by session ID/query", "r")
		fs.Lookup("resume").NoOptDefVal = resumePickerSentinel
		if err := fs.Parse(normalizeOptionalResumeArg(tc.args)); err != nil {
			t.Errorf("Parse(%v) err = %v", tc.args, err)
			continue
		}
		if *cont != tc.wantCont || *resume != tc.wantResume || *copySession != tc.wantCopy {
			t.Errorf("Parse(%v) = (cont=%v, resume=%q, copy=%v), want (cont=%v, resume=%q, copy=%v)",
				tc.args, *cont, *resume, *copySession, tc.wantCont, tc.wantResume, tc.wantCopy)
		}
	}

	// run registration: no -r shorthand, but --resume still works.
	fsRun := pflag.NewFlagSet("run", pflag.ContinueOnError)
	contRun, resumeRun, _ := registerResumeFlags(fsRun, "resume a specific session file", "")
	if err := fsRun.Parse([]string{"--resume", "some-path.jsonl", "-c"}); err != nil {
		t.Fatalf("run Parse(--resume ... -c) err = %v", err)
	}
	if !*contRun || *resumeRun != "some-path.jsonl" {
		t.Fatalf("run Parse = (cont=%v, resume=%q), want (true, %q)", *contRun, *resumeRun, "some-path.jsonl")
	}
	if err := fsRun.Parse([]string{"-r"}); err == nil {
		t.Fatal("run Parse(-r) should fail: run has no -r shorthand")
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
