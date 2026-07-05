package builtin

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/sandbox"
)

func TestBashHereDocIssue5624CommandsReturnPromptly(t *testing.T) {
	sh := requireHereDocBash(t)
	prevBashShellPATH := bashShellPATH
	bashShellPATH = func(context.Context) string { return "" }
	t.Cleanup(func() { bashShellPATH = prevBashShellPATH })

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "app", "platform", "github"), 0o755); err != nil {
		t.Fatalf("mkdir fixture: %v", err)
	}

	appendTarget := filepath.Join(root, "app", "platform", "github", "adapter.go")
	redactTarget := filepath.Join(root, "test_redact.go")
	tests := []struct {
		name       string
		command    string
		target     string
		wantOut    string
		wantInFile []string
	}{
		{
			name: "append pull request mapping heredoc",
			command: strings.Join([]string{
				"cat >> app/platform/github/adapter.go << 'EOF'",
				"",
				"// Pull request raw mapping (v0.6+).",
				"type githubPullRaw struct {",
				"\tNumber int    `json:\"number\"`",
				"\tTitle  string `json:\"title\"`",
				"}",
				"",
				"func githubPullToDetail(mergeable *bool) bool {",
				"\treturn mergeable != nil && *mergeable",
				"}",
				"EOF",
				"echo \"appended OK\"",
			}, "\n"),
			target:     appendTarget,
			wantOut:    "appended OK",
			wantInFile: []string{"type githubPullRaw struct", "`json:\"number\"`", "mergeable != nil && *mergeable"},
		},
		{
			name: "cd then write redact repro heredoc",
			command: strings.Join([]string{
				"cd " + bashQuote(filepath.ToSlash(root)) + " && cat > " + bashQuote(filepath.ToSlash(redactTarget)) + " <<'EOF'",
				"package main",
				"",
				"import (",
				"\t\"encoding/json\"",
				"\t\"fmt\"",
				")",
				"",
				"func main() {",
				"\tdata := []byte(`{\"accounts\":[{\"id\":\"a1\",\"username\":\"alice\",\"token\":\"TOKEN_EXAMPLE\"}]}`)",
				"\tvar v any",
				"\tjson.Unmarshal(data, &v)",
				"\tfmt.Printf(\"before: %v\\n\", v)",
				"}",
				"EOF",
				"echo \"skip\"",
			}, "\n"),
			target:     redactTarget,
			wantOut:    "skip",
			wantInFile: []string{"package main", "TOKEN_EXAMPLE", "fmt.Printf(\"before: %v\\n\", v)"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()

			done := make(chan struct {
				out     string
				err     error
				elapsed time.Duration
			}, 1)
			go func() {
				start := time.Now()
				out, err := (bash{shell: sh, workDir: root, timeout: 3 * time.Second}).Execute(ctx, argsJSON(t, map[string]any{"command": tt.command}))
				done <- struct {
					out     string
					err     error
					elapsed time.Duration
				}{out: out, err: err, elapsed: time.Since(start)}
			}()

			var got struct {
				out     string
				err     error
				elapsed time.Duration
			}
			select {
			case got = <-done:
			case <-time.After(10 * time.Second):
				t.Fatal("heredoc bash command did not return within 10s")
			}
			if got.err != nil {
				t.Fatalf("bash heredoc failed after %v: %v (out=%q)", got.elapsed, got.err, got.out)
			}
			if got.elapsed > 2*time.Second {
				t.Fatalf("bash heredoc returned too slowly: %v (out=%q)", got.elapsed, got.out)
			}
			if !strings.Contains(got.out, tt.wantOut) {
				t.Fatalf("output = %q, want %q", got.out, tt.wantOut)
			}
			data, err := os.ReadFile(tt.target)
			if err != nil {
				t.Fatalf("read heredoc target: %v", err)
			}
			body := string(data)
			for _, want := range tt.wantInFile {
				if !strings.Contains(body, want) {
					t.Fatalf("target missing %q:\n%s", want, body)
				}
			}
		})
	}
}

func requireHereDocBash(t *testing.T) sandbox.Shell {
	t.Helper()
	sh := sandbox.ResolveShell("bash", "", nil)
	if sh.Kind != sandbox.ShellBash {
		t.Skipf("bash heredoc regression requires bash, got %s", sh.Kind.String())
	}
	path := sh.Path
	if path == "" {
		path = "bash"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := exec.CommandContext(ctx, path, "-c", "true").Run(); err != nil {
		t.Skipf("bash heredoc regression requires a runnable bash: %v", err)
	}
	sh.Path = path
	return sh
}

func bashQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'"
}
