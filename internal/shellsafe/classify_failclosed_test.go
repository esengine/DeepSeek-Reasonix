package shellsafe

import (
	"reflect"
	"testing"
)

// TestClassifyReadOnlyCommandFailClosedMatrix drives the resolved dynamic
// path (resolvedReadOnlyStmt / resolvedReadOnlyWord / resolvedReadOnlyWordPart)
// through ClassifyReadOnlyCommand. Every shell construct that can smuggle a
// write past a read-only base word must fail closed: negation, backgrounding,
// disown, coprocesses, redirects (even to null sinks), assignments, and any
// substitution shape other than the single narrow double-quoted form.
func TestClassifyReadOnlyCommandFailClosedMatrix(t *testing.T) {
	cases := []struct {
		name string
		cmd  string
	}{
		// Negated commands.
		{"negated read-only", "! git status"},
		// Background jobs.
		{"backgrounded", "git status &"},
		{"backgrounded then disowned", "sleep 1 & disown"},
		// Coprocesses.
		{"coprocess clause", "coproc ls"},
		{"coprocess pipe", "ls |& tee out.txt"},
		// Redirects: any redirection is fail-closed, including null sinks and
		// fd duplication, which the classifier never strips.
		{"redirect stdout", "git status > log.txt"},
		{"redirect append", "ls >> log.txt"},
		{"redirect stderr", "cat x 2> err.txt"},
		{"redirect stdin", "ls < in.txt"},
		{"redirect to null sink", "ls >/dev/null"},
		{"fd dup", "git status 2>&1"},
		// Assignments.
		{"env assignment", "FOO=bar git status"},
		{"assignment with substitution", "FOO=$(pwd) ls -la"},
		{"assignment without command", "VAR=value"},
		// Multi-statement / chained / piped command substitution.
		{"multi-statement substitution", `basename "$(pwd; echo x)"`},
		{"chained substitution", `basename "$(pwd && echo x)"`},
		{"piped substitution", `basename "$(echo a | cat)"`},
		{"redirect inside substitution", `basename "$(echo a > out.txt)"`},
		{"null redirect inside substitution", `basename "$(ls > /dev/null)"`},
		{"write nested in substitution", `echo "$(rm -rf /)"`},
		{"write subcommand nested in substitution", `echo "$(git commit -m x)"`},
		// Substitution shapes outside the narrow allowed form.
		{"unquoted substitution", "basename $(pwd)"},
		{"backtick substitution", "echo `pwd`"},
		{"parameter expansion", `basename "$HOME"`},
		{"arithmetic expansion", `echo "$((1 + 2))"`},
		{"allowed substitution mixed with parameter", `echo "$(pwd) $HOME"`},
		// Statement structure.
		{"statement separator", "git status ; echo hi"},
		{"pipeline", "echo hi | cat"},
		{"subshell", "(ls -la)"},
		{"brace group", "{ ls -la; }"},
		{"if clause", "if true; then ls; fi"},
		{"for clause", `for f in .; do ls "$f"; done`},
		{"heredoc", "cat <<EOF\nhello\nEOF"},
		// nestedReadOnlyArgsSafe write-capable bases nested inside an allowed
		// substitution: all fail closed because these bases are not in
		// substitutionSafeCommands at all.
		{"nested find -exec", `basename "$(find . -exec rm {} \;)"`},
		{"nested find -delete", `dirname "$(find . -delete)"`},
		{"nested sed -i", `basename "$(sed -i s/x/y/ f)"`},
		{"nested sed read-only intent", `basename "$(sed -n 1p f)"`},
		{"nested sort --output", `basename "$(sort --output=out f)"`},
		{"nested git diff --output", `basename "$(git diff --output=patch.diff)"`},
		{"nested go env -w", `basename "$(go env -w GOFLAGS=-mod=vendor)"`},
		{"nested git tag create", `basename "$(git tag v1.0)"`},
		{"nested git tag -l", `basename "$(git tag -l)"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			base, sub, fields, ok := ClassifyReadOnlyCommand(tc.cmd)
			if ok {
				t.Errorf("ClassifyReadOnlyCommand(%q) = (%q, %q, %v, true), want fail-closed false",
					tc.cmd, base, sub, fields)
			}
		})
	}
}

// TestClassifyReadOnlyCommandNarrowDynamicShape pins the one allowed dynamic
// shape: a double-quoted command substitution whose nested command is a single,
// static, argument-safe read-only command. Fields carry the opaque
// substitution placeholder so consumers can still match args around it.
func TestClassifyReadOnlyCommandNarrowDynamicShape(t *testing.T) {
	cases := []struct {
		name       string
		cmd        string
		wantBase   string
		wantSub    string
		wantFields []string
	}{
		{"single substitution", `basename "$(pwd)"`, "basename", "",
			[]string{"basename", resolvedSubstitutionPlaceholder}},
		{"substitution with static arg", `dirname "$(realpath .)"`, "dirname", "",
			[]string{"dirname", resolvedSubstitutionPlaceholder}},
		// multiple single-stmt substitutions inside one word.
		{"multiple single-stmt substitutions", `echo "$(pwd) $(whoami)"`, "echo", "",
			[]string{"echo", resolvedSubstitutionPlaceholder + " " + resolvedSubstitutionPlaceholder}},
		{"substitution concatenated with literal", `head "$(pwd)/go.mod"`, "head", "",
			[]string{"head", resolvedSubstitutionPlaceholder + "/go.mod"}},
		{"read-only nested command", `cat "$(ls)"`, "cat", "",
			[]string{"cat", resolvedSubstitutionPlaceholder}},
		{"read-only nested echo", `basename "$(echo hi)"`, "basename", "",
			[]string{"basename", resolvedSubstitutionPlaceholder}},
		// Static commands keep the field-return form too.
		{"static base", "ls -la", "ls", "", []string{"ls", "-la"}},
		{"static subcommand", "git status", "git", "status", []string{"git", "status"}},
		{"static go env", "go env GOOS", "go", "env", []string{"go", "env", "GOOS"}},
		{"static npm view", "npm view react version", "npm", "view", []string{"npm", "view", "react", "version"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			base, sub, fields, ok := ClassifyReadOnlyCommand(tc.cmd)
			if !ok {
				t.Fatalf("ClassifyReadOnlyCommand(%q) = fail-closed, want ok", tc.cmd)
			}
			if base != tc.wantBase || sub != tc.wantSub || !reflect.DeepEqual(fields, tc.wantFields) {
				t.Errorf("ClassifyReadOnlyCommand(%q) = (%q, %q, %v, true), want (%q, %q, %v, true)",
					tc.cmd, base, sub, fields, tc.wantBase, tc.wantSub, tc.wantFields)
			}
		})
	}
}

// TestNestedReadOnlyArgsSafeAllowlist pins the nested-substitution argument
// allowlist table directly. These branches are unreachable through the public
// seam today — nestedReadOnlyArgsSafe is consulted only after
// substitutionSafeCommands admits the base, and find/sed/sort/git/go are not in
// that map, so nested substitutions of them fail closed before this table runs
// — but the table is the security boundary for those bases if the admission
// set ever widens, so it is tested as a unit.
func TestNestedReadOnlyArgsSafeAllowlist(t *testing.T) {
	restOf := func(base, sub string, rest ...string) []string {
		fields := []string{base}
		if sub != "" {
			fields = append(fields, sub)
		}
		return append(fields, rest...)
	}
	cases := []struct {
		name string
		base string
		sub  string
		rest []string
		want bool
	}{
		// find: -exec/-execdir/-delete/-ok/-okdir/-fls/-fprint/-fprint0/-fprintf
		// are write-capable; anything else is safe.
		{"find safe args", "find", "", []string{".", "-name", "*.go"}, true},
		{"find -exec", "find", "", []string{".", "-exec", "rm", "{}", ";"}, false},
		{"find -execdir", "find", "", []string{".", "-execdir", "rm", "{}", ";"}, false},
		{"find -delete", "find", "", []string{".", "-delete"}, false},
		{"find -ok", "find", "", []string{".", "-ok", "rm", "{}", ";"}, false},
		{"find -okdir", "find", "", []string{".", "-okdir", "rm", "{}", ";"}, false},
		{"find -fls", "find", "", []string{".", "-fls", "out"}, false},
		{"find -fprint", "find", "", []string{".", "-fprint", "out"}, false},
		{"find -fprint0", "find", "", []string{".", "-fprint0", "out"}, false},
		{"find -fprintf", "find", "", []string{".", "-fprintf", "out", "%p"}, false},
		{"find banned anywhere", "find", "", []string{".", "-name", "x", "-delete"}, false},
		// sed: -i / --in-place (with or without suffix) write in place.
		{"sed safe args", "sed", "", []string{"-n", "1p", "f"}, true},
		{"sed -i", "sed", "", []string{"-i", "s/x/y/", "f"}, false},
		{"sed -i suffix", "sed", "", []string{"-i.bak", "s/x/y/", "f"}, false},
		{"sed --in-place", "sed", "", []string{"--in-place", "s/x/y/", "f"}, false},
		{"sed --in-place suffix", "sed", "", []string{"--in-place=.bak", "s/x/y/", "f"}, false},
		// sort: -o / --output write a file.
		{"sort safe args", "sort", "", []string{"-k2", "f"}, true},
		{"sort -o", "sort", "", []string{"-o", "out", "f"}, false},
		{"sort -o attached", "sort", "", []string{"-oout", "f"}, false},
		{"sort --output", "sort", "", []string{"--output", "out", "f"}, false},
		{"sort --output=", "sort", "", []string{"--output=out", "f"}, false},
		// git diff/show/log: --output writes a patch file.
		{"git diff safe", "git", "diff", []string{"HEAD"}, true},
		{"git diff --output", "git", "diff", []string{"--output", "patch"}, false},
		{"git diff --output=", "git", "diff", []string{"--output=patch"}, false},
		{"git diff --stat", "git", "diff", []string{"--stat"}, true},
		{"git show --output=", "git", "show", []string{"--output=x"}, false},
		{"git log --output=", "git", "log", []string{"--output=x"}, false},
		{"git log safe", "git", "log", []string{"--oneline"}, true},
		// git tag: bare / -l / --list are listing; anything else creates a tag.
		{"git tag bare list", "git", "tag", nil, true},
		{"git tag -l", "git", "tag", []string{"-l"}, true},
		{"git tag --list", "git", "tag", []string{"--list", "v*"}, true},
		{"git tag create", "git", "tag", []string{"v1.0"}, false},
		{"git tag annotate", "git", "tag", []string{"-a", "v1.0"}, false},
		// go env: -w / -u mutate the go env file.
		{"go env safe", "go", "env", []string{"GOOS"}, true},
		{"go env -w", "go", "env", []string{"-w", "GOFLAGS=-mod=vendor"}, false},
		{"go env -u", "go", "env", []string{"-u", "GOFLAGS"}, false},
		// Bases/subcommands outside the table are unaffected.
		{"grep unrestricted", "grep", "", []string{"-r", "foo", "."}, true},
		{"cat unrestricted", "cat", "", []string{"x"}, true},
		{"git non-tag subcommand unaffected", "git", "branch", []string{"-a"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := nestedReadOnlyArgsSafe(tc.base, tc.sub, restOf(tc.base, tc.sub, tc.rest...))
			if got != tc.want {
				t.Errorf("nestedReadOnlyArgsSafe(%q, %q, %v) = %t, want %t",
					tc.base, tc.sub, tc.rest, got, tc.want)
			}
		})
	}
}
