package permission

import (
	"strings"
	"testing"
)

func TestNormalizeBashSafeRedirectsForMatch(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
		ok   bool
	}{
		{
			name: "stderr to dev null",
			in:   "git log 2>/dev/null",
			want: "git log",
			ok:   true,
		},
		{
			name: "stdout to dev null with space",
			in:   "git log > /dev/null",
			want: "git log",
			ok:   true,
		},
		{
			name: "append to dev null",
			in:   "git log >>/dev/null",
			want: "git log",
			ok:   true,
		},
		{
			name: "combined stdout stderr redirect",
			in:   "git log &>/dev/null",
			want: "git log",
			ok:   true,
		},
		{
			name: "combined append redirect",
			in:   "git log &>> /dev/null",
			want: "git log",
			ok:   true,
		},
		{
			name: "powershell null sink",
			in:   "git log >$null",
			want: "git log",
			ok:   true,
		},
		{
			name: "powershell null sink is case-insensitive",
			in:   "git log 2> $NULL",
			want: "git log",
			ok:   true,
		},
		{
			name: "windows nul sink",
			in:   "git log > NUL",
			want: "git log",
			ok:   true,
		},
		{
			name: "fd duplication",
			in:   "git log 2>&1",
			want: "git log",
			ok:   true,
		},
		{
			name: "fd duplication target must be a full word",
			in:   "git log 2>&1rm",
			ok:   false,
		},
		{
			name: "fd close",
			in:   "git log 2>&-",
			want: "git log",
			ok:   true,
		},
		{
			name: "fd close target must be a full word",
			in:   "git log 2>&-x",
			ok:   false,
		},
		{
			name: "file output stays unsafe",
			in:   "git log > changes.patch",
			ok:   false,
		},
		{
			name: "dev null prefix is not enough",
			in:   "git log >/dev/null.log",
			ok:   false,
		},
		{
			name: "powershell null prefix is not enough",
			in:   "git log >$nullish",
			ok:   false,
		},
		{
			name: "windows nul prefix is not enough",
			in:   "git log >nul.txt",
			ok:   false,
		},
		{
			name: "input redirection remains conservative",
			in:   "git log < /dev/null",
			ok:   false,
		},
		{
			name: "quoted redirect text is not removed",
			in:   "printf '%s\\n' '>/dev/null'",
			want: "printf '%s\\n' '>/dev/null'",
			ok:   true,
		},
		{
			name: "heredoc fails closed",
			in:   "cat <<'EOF'\n>/dev/null\nEOF",
			ok:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := normalizeBashSafeRedirectsForMatch(tt.in)
			if ok != tt.ok {
				t.Fatalf("ok = %v, want %v (got %q)", ok, tt.ok, got)
			}
			if ok && got != tt.want {
				t.Fatalf("normalized = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNormalizeBashSafeRedirectsPreservesControlSyntax(t *testing.T) {
	got, ok := normalizeBashSafeRedirectsForMatch("git log >/dev/null\nrm -rf /tmp/x")
	if !ok {
		t.Fatal("safe redirect should normalize while preserving the newline")
	}
	if !strings.Contains(got, "\n") {
		t.Fatalf("normalized command lost the control newline: %q", got)
	}
	if !containsShellSyntax(got) {
		t.Fatalf("normalized command should still be treated as shell syntax: %q", got)
	}
}
