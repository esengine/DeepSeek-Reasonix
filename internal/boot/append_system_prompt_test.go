package boot

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"reasonix/internal/config"
)

func TestAppendExternalSystemPrompt(t *testing.T) {
	cases := []struct {
		name, base, content, want string
	}{
		{"plain", "BASE", "HOST", "BASE\n\nHOST"},
		{"existing newlines", "BASE\n\n\n", "HOST", "BASE\n\nHOST"},
		{"exact UTF-8", "BASE", "\n  主机 guidance\r\n\n", "BASE\n\n\n  主机 guidance\r\n\n"},
		{"empty base", "", "HOST\n", "HOST\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "private-prompt.md")
			if err := os.WriteFile(path, []byte(tc.content), 0o600); err != nil {
				t.Fatal(err)
			}
			got, err := appendExternalSystemPrompt(tc.base, path)
			if err != nil || got != tc.want {
				t.Fatalf("appendExternalSystemPrompt = %q, %v; want %q", got, err, tc.want)
			}
		})
	}
	t.Run("no option preserves base", func(t *testing.T) {
		const base = "BASE\n\n"
		got, err := appendExternalSystemPrompt(base, "")
		if err != nil || got != base {
			t.Fatalf("appendExternalSystemPrompt = %q, %v; want unchanged base", got, err)
		}
	})
}

func TestAppendExternalSystemPromptRejectsInvalidFile(t *testing.T) {
	cases := []struct {
		name    string
		content []byte
	}{
		{"missing", nil},
		{"directory", nil},
		{"empty", []byte{}},
		{"whitespace", []byte(" \t\r\n")},
		{"invalid UTF-8", []byte("PRIVATE-CONTENT\xff")},
		{"unreadable", []byte("PRIVATE-CONTENT")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "private-prompt.md")
			if tc.name == "directory" {
				if err := os.Mkdir(path, 0o700); err != nil {
					t.Fatal(err)
				}
			} else if tc.content != nil {
				if err := os.WriteFile(path, tc.content, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if tc.name == "unreadable" {
				if runtime.GOOS == "windows" || os.Geteuid() == 0 {
					t.Skip("file mode does not deny reads for this user")
				}
				if err := os.Chmod(path, 0); err != nil {
					t.Fatal(err)
				}
			}
			got, err := appendExternalSystemPrompt("BASE", path)
			if err == nil {
				t.Fatal("invalid prompt file was accepted")
			}
			if got != "" {
				t.Fatal("invalid prompt file returned a usable system prompt")
			}
			assertExternalPromptErrorPrivate(t, err, path)
		})
	}
}

func TestBuildAppendsExternalSystemPrompt(t *testing.T) {
	root, opts := externalPromptFixture(t)
	writeFile(t, root, "AGENTS.md", "PROJECT STANDING INSTRUCTIONS")
	const content = "  PRIVATE-CONTENT 主机\r\n"
	writeExternalPrompt(t, opts.AppendSystemPromptFile, content)
	res := buildExternalPrompt(t, opts)
	prompt := systemMessage(res.Controller.History())
	last := -1
	for _, part := range []string{"BASE SYSTEM PROMPT", config.UserDecisionPolicy, config.WorkPracticePolicy, "PROJECT STANDING INSTRUCTIONS", content} {
		index := strings.Index(prompt, part)
		if index <= last {
			t.Fatalf("prompt missing or misordered component %q", part)
		}
		last = index
	}
	if strings.Count(prompt, content) != 1 || prompt != res.Snapshot.SystemPrompt() {
		t.Fatal("controller and snapshot must contain exactly one unchanged external block")
	}
	if strings.Contains(prompt, opts.AppendSystemPromptFile) {
		t.Fatal("prompt includes the private source path")
	}
	second := buildExternalPrompt(t, opts)
	if second.Snapshot.CacheHash() != res.Snapshot.CacheHash() {
		t.Fatal("unchanged external instructions changed the prompt cache fingerprint")
	}
}

func externalPromptFixture(t *testing.T) (string, Options) {
	t.Helper()
	isolateConfigHome(t)
	t.Setenv("REASONIX_HOME", robustTempDir(t))
	root := robustTempDir(t)
	t.Chdir(root)
	writeRuntimeFixture(t, root)
	path := filepath.Join(t.TempDir(), "private-prompt.md")
	writeExternalPrompt(t, path, "PRIVATE-CONTENT")
	return root, Options{WorkspaceRoot: root, AppendSystemPromptFile: path}
}

func writeExternalPrompt(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func buildExternalPrompt(t *testing.T, opts Options) *BuildResult {
	t.Helper()
	res, err := BuildRuntime(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(res.Controller.Close)
	return res
}

func assertExternalPromptErrorPrivate(t *testing.T, err error, path string) {
	t.Helper()
	for _, sensitive := range []string{path, filepath.Base(path), "PRIVATE-CONTENT"} {
		if strings.Contains(err.Error(), sensitive) {
			t.Fatalf("error exposed private prompt data: %v", err)
		}
	}
}
