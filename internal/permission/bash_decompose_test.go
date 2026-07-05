package permission

import (
	"reflect"
	"testing"
)

func TestDecomposeBashCommand(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{
			name: "atomic command returns nil",
			in:   "git status",
			want: nil,
		},
		{
			name: "atomic with redirect (has shell syntax but no operator) returns nil",
			in:   "grep -r TODO . 2>/dev/null",
			want: nil,
		},
		{
			name: "&& chain",
			in:   `git add . && git commit -m "wip" && git push`,
			want: []string{"git add .", `git commit -m "wip"`, "git push"},
		},
		{
			name: "|| fallback",
			in:   `sudo chmod 644 /etc/foo || echo "chmod failed"`,
			want: []string{"sudo chmod 644 /etc/foo", `echo "chmod failed"`},
		},
		{
			name: "pipe",
			in:   "git log --oneline | head -20",
			want: []string{"git log --oneline", "head -20"},
		},
		{
			name: "semicolon",
			in:   "cd /tmp; ls -la",
			want: []string{"cd /tmp", "ls -la"},
		},
		{
			name: "mixed compound",
			in:   `sudo chmod 644 /etc/ssh/foo 2>/dev/null || echo "sudo not available, trying alternative" && ssh -T git@github.com 2>&1`,
			want: []string{
				"sudo chmod 644 /etc/ssh/foo 2>/dev/null",
				`echo "sudo not available, trying alternative"`,
				"ssh -T git@github.com 2>&1",
			},
		},
		{
			name: "operator inside single quotes stays intact",
			in:   `echo 'a && b' && ls`,
			want: []string{`echo 'a && b'`, "ls"},
		},
		{
			name: "operator inside double quotes stays intact",
			in:   `echo "x | y" | wc -l`,
			want: []string{`echo "x | y"`, "wc -l"},
		},
		{
			name: "operator inside $(...) stays intact",
			in:   `echo $(git rev-parse HEAD; date) && ls`,
			want: []string{`echo $(git rev-parse HEAD; date)`, "ls"},
		},
		{
			name: "operator inside backticks stays intact",
			in:   "echo `git status; ls` && date",
			want: []string{"echo `git status; ls`", "date"},
		},
		{
			name: "2>&1 redirection is not a splitter",
			in:   "go test ./... 2>&1 | tee log",
			want: []string{"go test ./... 2>&1", "tee log"},
		},
		{
			name: "&>/dev/null redirection is not a splitter",
			in:   "git log &>/dev/null | head -20",
			want: []string{"git log &>/dev/null", "head -20"},
		},
		{
			name: "leading &>/dev/null redirection is not malformed",
			in:   "&>/dev/null git log | head -20",
			want: []string{"&>/dev/null git log", "head -20"},
		},
		{
			name: "empty tail after trailing operator is dropped",
			in:   "ls -la;",
			want: nil, // only one non-empty segment after split
		},
		{
			name: "unclosed quote returns nil (falls back to exact)",
			in:   `echo "hello && ls`,
			want: nil,
		},
		{
			name: "unclosed $(...) returns nil",
			in:   "echo $(git status && ls",
			want: nil,
		},
		{
			name: "newline splits",
			in:   "cd /tmp\nls",
			want: []string{"cd /tmp", "ls"},
		},
		{
			name: "heredoc bails to nil (known out-of-scope)",
			in:   "cat <<EOF && ls\nline1\nEOF",
			want: nil,
		},
		{
			name: "leading && is malformed, returns nil",
			in:   "&& ls",
			want: nil,
		},
		{
			name: "leading || is malformed, returns nil",
			in:   "|| echo hi",
			want: nil,
		},
		{
			name: "leading ; is malformed, returns nil",
			in:   "; ls",
			want: nil,
		},
		{
			name: "leading | is malformed, returns nil",
			in:   "| grep foo",
			want: nil,
		},
		{
			name: "process substitution <(cmd) is opaque, operators inside don't split",
			in:   "diff <(git log -1 | head) <(git show HEAD | head) && ls",
			want: []string{
				"diff <(git log -1 | head) <(git show HEAD | head)",
				"ls",
			},
		},
		{
			name: "process substitution >(cmd) is opaque",
			in:   "tee >(gzip | tar) && date",
			want: []string{"tee >(gzip | tar)", "date"},
		},
		{
			name: "single < is redirect, stays with segment",
			in:   "sort < input.txt && ls",
			want: []string{"sort < input.txt", "ls"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DecomposeBashCommand(tt.in)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("DecomposeBashCommand(%q)\n  got:  %#v\n  want: %#v", tt.in, got, tt.want)
			}
		})
	}
}

func TestPolicyDecideCompoundBash(t *testing.T) {
	// Simulate a user who has approved `git add`, `git commit`, `git push`
	// atomically at some earlier point — either via config or via the
	// prefix-rule save path that already exists.
	p := New("ask", []string{
		"Bash(git add:*)",
		"Bash(git commit:*)",
		"Bash(git push:*)",
		"Bash(go test:*)",
		"Bash(sudo chmod:*)",
	}, nil, []string{
		"Bash(rm -rf*)",
	})

	cases := []struct {
		name    string
		subject string
		want    Decision
	}{
		{
			name:    "compound of atomic-allowed segments passes",
			subject: `git add . && git commit -m "wip" && git push`,
			want:    Allow,
		},
		{
			name:    "one uncovered segment turns into Ask",
			subject: `git add . && git commit -m "wip" && git push && npm publish`,
			want:    Ask,
		},
		{
			name:    "deny in any segment wins",
			subject: `git add . && rm -rf /tmp/scratch`,
			want:    Deny,
		},
		{
			name:    "read-only segments auto-allow without a rule",
			subject: `echo starting && git add . && ls -la`,
			want:    Allow,
		},
		{
			name:    "compound with || also passes when segments have no redirects",
			subject: `sudo chmod 644 /etc/foo || echo "chmod failed"`,
			// sudo chmod ...  → matches Bash(sudo chmod:*)
			// echo "..."       → readonly builtin
			want: Allow,
		},
		{
			name:    "segment with dev null redirect still matches prefix rule",
			subject: `sudo chmod 644 /etc/foo 2>/dev/null || echo "chmod failed"`,
			want:    Allow,
		},
		{
			name:    "segment with file redirect still misses prefix rule",
			subject: `sudo chmod 644 /etc/foo > chmod.log || echo "chmod failed"`,
			want:    Ask,
		},
		{
			name:    "segment with fd duplication still matches prefix rule",
			subject: `go test ./... 2>&1 | head -20`,
			want:    Allow,
		},
		{
			name:    "read-only segment with dev null redirect auto-allows",
			subject: `git log --oneline 2>/dev/null | head -20`,
			want:    Allow,
		},
		{
			name:    "write-capable read-only-looking arg still asks after safe redirect",
			subject: `git diff --output changes.patch 2>/dev/null | head -20`,
			want:    Ask,
		},
		{
			name:    "atomic subject with matching prefix rule still allows",
			subject: "git push origin main",
			want:    Allow,
		},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			got := p.DecideSubject("bash", false, tt.subject)
			if got != tt.want {
				t.Errorf("DecideSubject(%q) = %v, want %v", tt.subject, got, tt.want)
			}
		})
	}
}

func TestPolicyDecideCompoundBashPreservesWholeCommandRules(t *testing.T) {
	subject := `git add . && git commit -m "wip" && git push`

	t.Run("exact allow still wins before segment decomposition", func(t *testing.T) {
		p := New("ask", []string{`Bash(git add . && git commit -m "wip" && git push)`}, nil, nil)
		if got := p.DecideSubject("bash", false, subject); got != Allow {
			t.Fatalf("DecideSubject(%q) = %v, want %v", subject, got, Allow)
		}
	})

	t.Run("exact deny still beats segment allows", func(t *testing.T) {
		p := New("ask", []string{
			"Bash(git add:*)",
			"Bash(git commit:*)",
			"Bash(git push:*)",
		}, nil, []string{`Bash(git add . && git commit -m "wip" && git push)`})
		if got := p.DecideSubject("bash", false, subject); got != Deny {
			t.Fatalf("DecideSubject(%q) = %v, want %v", subject, got, Deny)
		}
	})

	t.Run("exact ask still beats segment allows", func(t *testing.T) {
		p := New("allow", []string{
			"Bash(git add:*)",
			"Bash(git commit:*)",
			"Bash(git push:*)",
		}, []string{`Bash(git add . && git commit -m "wip" && git push)`}, nil)
		if got := p.DecideSubject("bash", false, subject); got != Ask {
			t.Fatalf("DecideSubject(%q) = %v, want %v", subject, got, Ask)
		}
	})
}
