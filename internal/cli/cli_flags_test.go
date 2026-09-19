package cli

import (
	"reflect"
	"strings"
	"testing"

	"github.com/spf13/pflag"
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

func TestResolveSessionQueryByIDAndPreview(t *testing.T) {
	dir := t.TempDir()
	first := seedNativeTestSession(t, dir, "fix provider configuration")
	seedNativeTestSession(t, dir, "improve terminal picker")

	got, err := resolveSessionQuery(dir, first)
	if err != nil || got != v4ResumeLocator(first) {
		t.Fatalf("resolve by ID = (%q, %v), want %q", got, err, v4ResumeLocator(first))
	}
	got, err = resolveSessionQuery(dir, "provider configuration")
	if err != nil || got != v4ResumeLocator(first) {
		t.Fatalf("resolve by preview = (%q, %v), want %q", got, err, v4ResumeLocator(first))
	}
	if _, err := resolveSessionQuery(dir, "missing"); err == nil || !strings.Contains(err.Error(), "no session") {
		t.Fatalf("missing query error = %v", err)
	}
}
