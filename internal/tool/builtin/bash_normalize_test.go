package builtin

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"reasonix/internal/sandbox"
)

func TestNormalizeBashCommand(t *testing.T) {
	absWork := func(p string) string {
		abs, err := filepath.Abs(p)
		if err != nil {
			t.Fatalf("abs: %v", err)
		}
		return filepath.ToSlash(abs)
	}
	work := absWork("/workspace")
	workSub := absWork("/workspace/subdir")
	workWithSpace := absWork("/workspace with space")
	workUnicode := absWork("/workspace/项目")

	tests := []struct {
		name    string
		command string
		workDir string
		want    string
	}{
		// === no-op ===
		{name: "simple", command: "ls -la", workDir: work, want: "ls -la"},
		{name: "empty", command: "", workDir: work, want: ""},
		{name: "no workDir", command: "cd /w && go build", workDir: "", want: "cd /w && go build"},
		{name: "just command", command: "go test ./...", workDir: work, want: "go test ./..."},
		{name: "unparseable", command: "for i in *; do echo $i; done", workDir: work, want: "for i in *; do echo $i; done"},
		{name: "stderr_to_file", command: "cmd 2>/dev/null", workDir: work, want: "cmd 2>/dev/null"},
		{name: "preserve_outer_whitespace", command: "  printf '%s\\n' foo  ", workDir: work, want: "  printf '%s\\n' foo  "},
		{name: "preserve_comment", command: "printf '%s\\n' foo # keep me", workDir: work, want: "printf '%s\\n' foo # keep me"},
		{name: "preserve_line_continuation", command: "printf '%s\\n' \\\n  foo", workDir: work, want: "printf '%s\\n' \\\n  foo"},
		{name: "preserve_and_chain_spacing", command: "echo first  &&  echo second", workDir: work, want: "echo first  &&  echo second"},

		// === cd stripping ===
		{name: "cd_match", command: "cd " + work + " && go build", workDir: work, want: "go build"},
		{name: "cd_match_double_quoted", command: `cd "` + work + `" && go build`, workDir: work, want: "go build"},
		{name: "cd_match_single_quoted", command: `cd '` + work + `' && go build`, workDir: work, want: "go build"},
		{name: "cd_mismatch", command: "cd /other && go build", workDir: work, want: "cd /other && go build"},
		{name: "cd_subdir", command: "cd " + workSub + " && go build", workDir: work, want: "cd " + workSub + " && go build"},
		{name: "cd_relative", command: "cd ../other && go build", workDir: work, want: "cd ../other && go build"},
		{name: "cd_only", command: "cd " + work, workDir: work, want: ""},
		{name: "cd_path_with_space", command: `cd '` + workWithSpace + `' && go build`, workDir: workWithSpace, want: "go build"},
		{name: "cd_unicode_path", command: `cd '` + workUnicode + `' && go build`, workDir: workUnicode, want: "go build"},
		{name: "cd_dynamic_variable", command: `cd "$WORKSPACE" && go build`, workDir: work, want: `cd "$WORKSPACE" && go build`},
		{name: "cd_dynamic_command_substitution", command: `cd "$(pwd)" && go build`, workDir: work, want: `cd "$(pwd)" && go build`},
		{name: "cd_with_option", command: "cd -- " + work + " && go build", workDir: work, want: "cd -- " + work + " && go build"},
		{name: "cd_before_or", command: "cd " + work + " || go build", workDir: work, want: "cd " + work + " || go build"},

		// === 2>&1 stripping ===
		{name: "stderr_merge", command: "go build ./... 2>&1", workDir: work, want: "go build ./..."},
		{name: "stderr_merge_quoted_not_stripped", command: `echo "2>&1"`, workDir: work, want: `echo "2>&1"`},
		{name: "stderr_merge_preserves_quoted_space", command: `printf '%s\n' 'hello world' 2>&1`, workDir: work, want: `printf '%s\n' 'hello world'`},
		{name: "stderr_merge_preserves_empty_arg", command: `printf '<%s>' '' 2>&1`, workDir: work, want: `printf '<%s>' ''`},
		{name: "stderr_merge_preserves_literal_glob", command: `printf '%s\n' '*' 2>&1`, workDir: work, want: `printf '%s\n' '*'`},
		{name: "stderr_merge_preserves_literal_variable", command: `printf '%s\n' '$HOME' 2>&1`, workDir: work, want: `printf '%s\n' '$HOME'`},
		{name: "stderr_merge_preserves_literal_command_substitution", command: `printf '%s\n' '$(touch /tmp/pwn)' 2>&1`, workDir: work, want: `printf '%s\n' '$(touch /tmp/pwn)'`},
		{name: "stderr_merge_preserves_literal_semicolon", command: `printf '%s\n' '; rm -rf /tmp/x' 2>&1`, workDir: work, want: `printf '%s\n' '; rm -rf /tmp/x'`},
		{name: "stderr_merge_preserves_literal_and", command: `printf '%s\n' '&& rm -rf /tmp/x' 2>&1`, workDir: work, want: `printf '%s\n' '&& rm -rf /tmp/x'`},
		{name: "stderr_merge_preserves_dynamic_variable", command: `printf '%s\n' "$HOME" 2>&1`, workDir: work, want: `printf '%s\n' "$HOME"`},
		{name: "stderr_merge_preserves_dynamic_command_substitution", command: `printf '%s\n' "$(whoami)" 2>&1`, workDir: work, want: `printf '%s\n' "$(whoami)"`},
		{name: "stderr_merge_preserves_dynamic_array", command: `printf '%s\n' "${items[@]}" 2>&1`, workDir: work, want: `printf '%s\n' "${items[@]}"`},
		{name: "stderr_merge_preserves_dynamic_glob", command: `printf '%s\n' *.go 2>&1`, workDir: work, want: `printf '%s\n' *.go`},
		{name: "stderr_merge_spaced_duplication", command: "go build 2>& 1", workDir: work, want: "go build"},

		// === redirects whose order or target is significant ===
		{name: "stdout_file_then_stderr_merge", command: "go build >out 2>&1", workDir: work, want: "go build >out 2>&1"},
		{name: "stderr_merge_then_stdout_file", command: "go build 2>&1 >out", workDir: work, want: "go build 2>&1 >out"},
		{name: "stderr_merge_before_arg", command: "go build 2>&1 ./...", workDir: work, want: "go build 2>&1 ./..."},
		{name: "stderr_to_stderr", command: "go build 2>&2", workDir: work, want: "go build 2>&2"},
		{name: "other_fd_to_stdout", command: "go build 3>&1", workDir: work, want: "go build 3>&1"},
		{name: "stderr_merge_before_comment", command: "go build 2>&1 # keep me", workDir: work, want: "go build 2>&1 # keep me"},

		// === combined stripping ===
		{name: "cd_and_stderr_merge", command: "cd " + work + " && go build ./... 2>&1", workDir: work, want: "go build ./..."},

		// === partial strip ===
		{name: "cd_subdir_and_stderr", command: "cd " + work + " && cd subdir && go build ./... 2>&1", workDir: work, want: "cd subdir && go build ./..."},
		{name: "cd_and_stderr_then_more", command: "cd " + work + " && go build ./... 2>&1 && echo done", workDir: work, want: "go build ./... && echo done"},
		{name: "stderr_merge_each_and_segment", command: "echo first 2>&1 && echo second 2>&1", workDir: work, want: "echo first && echo second"},
		{name: "quoted_args_in_and_chain", command: `printf '%s\n' '$HOME' 2>&1 && printf '%s\n' 'hello world' 2>&1`, workDir: work, want: `printf '%s\n' '$HOME' && printf '%s\n' 'hello world'`},

		// === 2>&1 not at end → kept ===
		{name: "stderr_merge_mid_pipe", command: "go build 2>&1 | grep error", workDir: work, want: "go build 2>&1 | grep error"},
		{name: "stderr_merge_before_or", command: "go build 2>&1 || echo failed", workDir: work, want: "go build 2>&1 || echo failed"},
		{name: "pipe_stderr_operator", command: "go build |& grep error", workDir: work, want: "go build |& grep error"},

		// === cd not first → kept ===
		{name: "cd_not_first", command: "echo hi && cd " + work + " && go build", workDir: work, want: "echo hi && cd " + work + " && go build"},

		// === multi-line with && (single statement, works) ===
		{name: "multiline_and", command: "cd " + work + " &&\ngo build ./... 2>&1", workDir: work, want: "go build ./..."},
		{name: "multiline_and_backslash", command: "cd " + work + " && \\\ngo build ./... 2>&1", workDir: work, want: "go build ./..."},

		// === multi-line without && or with ; (two statements, fail-safe) ===
		{name: "multiline_bare", command: "cd " + work + "\ngo build", workDir: work, want: "cd " + work + "\ngo build"},
		{name: "semicolon", command: "cd " + work + "; go build", workDir: work, want: "cd " + work + "; go build"},

		// === unsupported compound forms fail closed ===
		{name: "subshell", command: "(printf '%s\\n' hi) 2>&1", workDir: work, want: "(printf '%s\\n' hi) 2>&1"},
		{name: "brace_group", command: "{ printf '%s\\n' hi; } 2>&1", workDir: work, want: "{ printf '%s\\n' hi; } 2>&1"},
		{name: "negated", command: "! go build 2>&1", workDir: work, want: "! go build 2>&1"},
		{name: "background", command: "go build 2>&1 &", workDir: work, want: "go build 2>&1 &"},
		{name: "heredoc", command: "cat <<'EOF' 2>&1\n$HOME && rm -rf /tmp/x\nEOF", workDir: work, want: "cat <<'EOF' 2>&1\n$HOME && rm -rf /tmp/x\nEOF"},
		{name: "unterminated_quote", command: `printf '%s`, workDir: work, want: `printf '%s`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeBashCommand(tt.command, tt.workDir)
			if got != tt.want {
				t.Errorf("normalizeBashCommand(%q, %q) = %q, want %q", tt.command, tt.workDir, got, tt.want)
			}
			if twice := normalizeBashCommand(got, tt.workDir); twice != got {
				t.Errorf("normalizeBashCommand is not idempotent: first = %q, second = %q", got, twice)
			}
		})
	}
}

func TestNormalizeCommandUsesActualShell(t *testing.T) {
	work, err := filepath.Abs("/workspace")
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	work = filepath.ToSlash(work)
	command := "cd " + work + " && go test ./... 2>&1"

	tests := []struct {
		name  string
		shell sandbox.Shell
		want  string
	}{
		{
			name:  "bash normalizes",
			shell: sandbox.Shell{Kind: sandbox.ShellBash, Path: "bash"},
			want:  "go test ./...",
		},
		{
			name:  "PowerShell preserves source",
			shell: sandbox.Shell{Kind: sandbox.ShellPowerShell, Path: "powershell"},
			want:  command,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := bash{workDir: work, shell: tt.shell}
			if got := b.normalizeCommand(command); got != tt.want {
				t.Fatalf("normalizeCommand() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNormalizePermissionArgsPreservesNonCommandFields(t *testing.T) {
	work, err := filepath.Abs("/workspace")
	if err != nil {
		t.Fatalf("abs: %v", err)
	}
	work = filepath.ToSlash(work)
	b := bash{workDir: work, shell: sandbox.Shell{Kind: sandbox.ShellBash, Path: "bash"}}
	params := bashParams{
		Command:                     "cd " + work + " && printf '%s\\n' 'hello world' 2>&1",
		RunInBackground:             true,
		PreserveBackgroundProcesses: true,
		SandboxCapabilities:         json.RawMessage(`{"network":true,"justification":"test"}`),
	}
	raw, err := json.Marshal(params)
	if err != nil {
		t.Fatal(err)
	}

	var got bashParams
	if err := json.Unmarshal(b.NormalizePermissionArgs(raw), &got); err != nil {
		t.Fatalf("unmarshal normalized args: %v", err)
	}
	if want := `printf '%s\n' 'hello world'`; got.Command != want {
		t.Errorf("Command = %q, want %q", got.Command, want)
	}
	if !got.RunInBackground {
		t.Error("RunInBackground = false, want true")
	}
	if !got.PreserveBackgroundProcesses {
		t.Error("PreserveBackgroundProcesses = false, want true")
	}
	if got := string(got.SandboxCapabilities); got != `{"network":true,"justification":"test"}` {
		t.Errorf("SandboxCapabilities = %s, want preserved object", got)
	}
}

func TestNormalizePermissionArgsMalformedJSONIsUnchanged(t *testing.T) {
	b := bash{workDir: "/workspace"}
	raw := json.RawMessage(`{"command":`)
	if got := b.NormalizePermissionArgs(raw); string(got) != string(raw) {
		t.Errorf("NormalizePermissionArgs(%q) = %q, want input unchanged", raw, got)
	}
}
