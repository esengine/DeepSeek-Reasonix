package shellparse

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestStaticFields(t *testing.T) {
	tests := []struct {
		name      string
		command   string
		want      []string
		malformed bool
	}{
		{
			name:    "plain command",
			command: "git status --short",
			want:    []string{"git", "status", "--short"},
		},
		{
			name:    "quoted static fields",
			command: `grep 'a|b' "file name.txt"`,
			want:    []string{"grep", "a|b", "file name.txt"},
		},
		{
			name:    "escaped static field",
			command: `find . -name scratch \-delete`,
			want:    []string{"find", ".", "-name", "scratch", "-delete"},
		},
		{
			name:      "redirect is shell syntax",
			command:   "git log >/dev/null",
			malformed: true,
		},
		{
			name:      "control operator is shell syntax",
			command:   "git status && rm -rf /tmp/x",
			malformed: true,
		},
		{
			name:      "parameter expansion is shell syntax",
			command:   "git diff $REV",
			malformed: true,
		},
		{
			name:      "command substitution is shell syntax",
			command:   "echo $(touch out)",
			malformed: true,
		},
		{
			name:      "assignment prefix is shell syntax",
			command:   "GIT_EXTERNAL_DIFF=cat git diff",
			malformed: true,
		},
		{
			name:      "parse failure is malformed",
			command:   "echo 'unterminated",
			malformed: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, malformed := StaticFields(tt.command)
			if tt.malformed {
				if malformed == "" {
					t.Fatalf("StaticFields(%q) malformed = empty, want error", tt.command)
				}
				return
			}
			if malformed != "" {
				t.Fatalf("StaticFields(%q) malformed = %q", tt.command, malformed)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("StaticFields(%q) = %#v, want %#v", tt.command, got, tt.want)
			}
		})
	}
}

func TestParseStaticCommandPolicy(t *testing.T) {
	got, err := ParseStaticCommand(`FOO=bar MESSAGE='hello world' go test ./...`, StaticCommandPolicy{AllowEnvAssignments: true})
	if err != nil {
		t.Fatalf("ParseStaticCommand env assignment: %v", err)
	}
	if !reflect.DeepEqual(got.Env, []string{"FOO=bar", "MESSAGE=hello world"}) {
		t.Fatalf("Env = %#v", got.Env)
	}
	if !reflect.DeepEqual(got.Argv, []string{"go", "test", "./..."}) {
		t.Fatalf("Argv = %#v", got.Argv)
	}

	_, err = ParseStaticCommand(`FOO=bar go test`, StaticCommandPolicy{})
	assertStaticRejectReason(t, err, StaticRejectAssignment)

	_, err = ParseStaticCommand(`FOO=$(whoami) go test`, StaticCommandPolicy{AllowEnvAssignments: true})
	assertStaticRejectReason(t, err, StaticRejectExpansion)

	_, err = ParseStaticCommand(`go test ./... >out.txt`, StaticCommandPolicy{AllowEnvAssignments: true})
	assertStaticRejectReason(t, err, StaticRejectRedirection)

	got, err = ParseStaticCommand(`go test ./... 2>&1`, StaticCommandPolicy{AllowStderrToStdout: true})
	if err != nil {
		t.Fatalf("ParseStaticCommand stderr merge: %v", err)
	}
	if !got.MergeStderr {
		t.Fatalf("MergeStderr = false, want true")
	}
	if !reflect.DeepEqual(got.Argv, []string{"go", "test", "./..."}) {
		t.Fatalf("Argv with stderr merge = %#v", got.Argv)
	}

	_, err = ParseStaticCommand(`go test ./... 2>err.txt`, StaticCommandPolicy{AllowStderrToStdout: true})
	assertStaticRejectReason(t, err, StaticRejectRedirection)

	if _, malformed := StaticFields(`FOO=bar go test`); malformed != "shell control syntax" {
		t.Fatalf("StaticFields assignment malformed = %q", malformed)
	}
	if _, malformed := StaticFields(`go test 2>&1`); malformed != "shell control syntax" {
		t.Fatalf("StaticFields redirection malformed = %q", malformed)
	}
}

func assertStaticRejectReason(t *testing.T, err error, want StaticRejectReason) {
	t.Helper()
	var reject *StaticRejectError
	if !errors.As(err, &reject) {
		t.Fatalf("error = %v (%T), want StaticRejectError", err, err)
	}
	if reject.Reason != want {
		t.Fatalf("reason = %q, want %q (err=%v)", reject.Reason, want, err)
	}
}

func TestContainsShellSyntax(t *testing.T) {
	for _, command := range []string{
		"git status && rm -rf /",
		"cat a | tee b",
		"git status > out.txt",
		"echo $(rm x)",
		"echo $HOME",
		"echo `whoami`",
		"sleep 1 &",
	} {
		if !ContainsShellSyntax(command) {
			t.Fatalf("ContainsShellSyntax(%q) = false, want true", command)
		}
	}
	for _, command := range []string{
		"git status",
		`grep 'a|b' file`,
		`printf "%s\n" "a && b"`,
		`find . -name scratch \-delete`,
	} {
		if ContainsShellSyntax(command) {
			t.Fatalf("ContainsShellSyntax(%q) = true, want false", command)
		}
	}
}

func TestSplitTopLevel(t *testing.T) {
	tests := []struct {
		name      string
		command   string
		want      []string
		wantSplit bool
		wantOK    bool
	}{
		{
			name:    "atomic command",
			command: "git status",
			want:    []string{"git status"},
			wantOK:  true,
		},
		{
			name:      "and chain",
			command:   `git add . && git commit -m "wip" && git push`,
			want:      []string{"git add .", `git commit -m "wip"`, "git push"},
			wantSplit: true,
			wantOK:    true,
		},
		{
			name:      "semicolon chain",
			command:   "cd /tmp; ls -la",
			want:      []string{"cd /tmp", "ls -la"},
			wantSplit: true,
			wantOK:    true,
		},
		{
			name:      "pipe",
			command:   "git log --oneline | head -20",
			want:      []string{"git log --oneline", "head -20"},
			wantSplit: true,
			wantOK:    true,
		},
		{
			name:      "background",
			command:   "sleep 1 & echo done",
			want:      []string{"sleep 1", "echo done"},
			wantSplit: true,
			wantOK:    true,
		},
		{
			name:      "operator inside quotes stays in segment",
			command:   `echo 'a && b' && ls`,
			want:      []string{`echo 'a && b'`, "ls"},
			wantSplit: true,
			wantOK:    true,
		},
		{
			name:      "command substitution is opaque",
			command:   `echo $(git rev-parse HEAD; date) && ls`,
			want:      []string{`echo $(git rev-parse HEAD; date)`, "ls"},
			wantSplit: true,
			wantOK:    true,
		},
		{
			name:    "process substitution is opaque",
			command: "diff <(git log -1 | head) <(git show HEAD | head) && ls",
			want: []string{
				"diff <(git log -1 | head) <(git show HEAD | head)",
				"ls",
			},
			wantSplit: true,
			wantOK:    true,
		},
		{
			name:    "comments are skipped",
			command: "# comment\nshuf -i 1-30 -n 10 | sort -rn",
			want: []string{
				"shuf -i 1-30 -n 10",
				"sort -rn",
			},
			wantSplit: true,
			wantOK:    true,
		},
		{
			name:    "heredoc fails closed",
			command: "cat <<EOF && ls\nline1\nEOF",
			wantOK:  false,
		},
		{
			name:    "compound statement fails closed",
			command: "if true; then ls; fi && pwd",
			wantOK:  false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, split, ok := SplitTopLevel(tt.command)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v (segments=%#v)", ok, tt.wantOK, got)
			}
			if !ok {
				return
			}
			if split != tt.wantSplit {
				t.Fatalf("split = %v, want %v", split, tt.wantSplit)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("segments = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestHasHereDoc(t *testing.T) {
	file, err := ParseBash(strings.Join([]string{
		"cat <<'EOF'",
		"nohup sleep 60 >/dev/null 2>&1 &",
		"EOF",
	}, "\n"))
	if err != nil {
		t.Fatalf("ParseBash heredoc: %v", err)
	}
	if !HasHereDoc(file) {
		t.Fatal("HasHereDoc = false, want true")
	}
}
