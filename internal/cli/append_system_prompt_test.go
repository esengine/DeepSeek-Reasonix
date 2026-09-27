package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveAppendSystemPromptFile(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	for _, tc := range []struct {
		name  string
		data  []byte
		valid bool
	}{
		{"unicode", []byte("Private guidance 世界\n"), true},
		{"empty", nil, false},
		{"whitespace", []byte(" \n\t"), false},
		{"invalid-utf8", []byte{0xff, 0xfe}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			name := "sensitive-" + tc.name
			if err := os.WriteFile(name, tc.data, 0o600); err != nil {
				t.Fatal(err)
			}
			for _, input := range []string{name, filepath.Join(dir, name)} {
				got, err := resolveAppendSystemPromptFile(input)
				if tc.valid {
					if err != nil || !filepath.IsAbs(got) {
						t.Fatalf("path=%q err=%v", got, err)
					}
					data, err := os.ReadFile(got)
					if err != nil || string(data) != string(tc.data) {
						t.Fatalf("resolved wrong file: %v", err)
					}
				} else if err == nil || strings.Contains(err.Error(), name) || got != "" {
					t.Fatalf("invalid file accepted or disclosed: path=%q err=%v", got, err)
				}
			}
		})
	}
	for _, input := range []string{"", "sensitive-missing", dir} {
		if got, err := resolveAppendSystemPromptFile(input); err == nil || got != "" || (input != "" && strings.Contains(err.Error(), input)) {
			t.Fatalf("invalid input accepted or disclosed: path=%q err=%v", got, err)
		}
	}
}

func TestAppendSystemPromptFlagRejectsInvalidFile(t *testing.T) {
	isolateCLIConfigHome(t)
	t.Chdir(t.TempDir())
	for _, prefix := range [][]string{nil, {"run"}, {"chat"}, {"-p"}} {
		for _, value := range []string{"sensitive-missing-file", ""} {
			args := append(append([]string{}, prefix...), "--append-system-prompt-file="+value)
			args = append(args, "task")
			var code int
			stdout, stderr := captureCLIOutput(t, func() { code = Run(args, "test-version") })
			if code != 2 || !strings.Contains(stderr, "system prompt") || strings.Contains(stderr, "unknown") {
				t.Fatalf("args=%q code=%d stdout=%q stderr=%q", args, code, stdout, stderr)
			}
			if strings.Contains(stdout+stderr, "sensitive-missing-file") {
				t.Fatal("private path disclosed")
			}
		}
	}
}

func TestAppendSystemPromptFlagMissingValue(t *testing.T) {
	isolateCLIConfigHome(t)
	for _, prefix := range [][]string{nil, {"run"}, {"chat"}} {
		args := append(append([]string{}, prefix...), "--append-system-prompt-file")
		var code int
		stderr := captureStderr(t, func() { code = Run(args, "test-version") })
		if code != 2 || !strings.Contains(stderr, "flag needs an argument") {
			t.Fatalf("code=%d stderr=%q", code, stderr)
		}
	}
}

func TestAppendSystemPromptCompletion(t *testing.T) {
	root := cliCompletionRootSpec()
	for _, words := range [][]string{{"reasonix", "--append"}, {"reasonix", "run", "--append"}, {"reasonix", "chat", "--append"}} {
		got := cliCompletionCandidatesWithValues(root, len(words)-1, words, func(cliCompletionValueKind) []string { return nil })
		if len(got) != 1 || got[0] != "--append-system-prompt-file" {
			t.Fatalf("completion=%v", got)
		}
		words[len(words)-1] = "--append-system-prompt-file"
		words = append(words, "")
		got = cliCompletionCandidatesWithValues(root, len(words)-1, words, func(kind cliCompletionValueKind) []string {
			if kind != cliCompletionPathValue {
				t.Errorf("completion kind=%v, want file path", kind)
			}
			return nil
		})
		if len(got) != 0 {
			t.Fatalf("path completion=%v", got)
		}
	}
}
