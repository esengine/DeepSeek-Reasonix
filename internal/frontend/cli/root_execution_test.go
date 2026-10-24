package cli

import (
	"context"
	"crypto/rand"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"reasonix/internal/contract/config"
	"reasonix/internal/contract/provider"
	"reasonix/internal/platform/gitcmd"
	"reasonix/internal/state/sessionstore"
	"reasonix/internal/state/store"
)

type rootRunProvider struct{ calls atomic.Int32 }

func (*rootRunProvider) Name() string { return "root-run" }
func (p *rootRunProvider) Stream(context.Context, provider.Request) (<-chan provider.Chunk, error) {
	ch := make(chan provider.Chunk, 2)
	if p.calls.Add(1) == 1 {
		ch <- provider.Chunk{Type: provider.ChunkToolCall, ToolCall: &provider.ToolCall{ID: "write", Name: "write_file", Arguments: `{"path":"from-run.txt","content":"isolated\n"}`}}
	} else {
		ch <- provider.Chunk{Type: provider.ChunkText, Text: "done"}
	}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

func rootRunFixture(t *testing.T) string {
	t.Helper()
	isolateCLIConfigHome(t)
	p := &rootRunProvider{}
	kind := "cli-root-" + strings.ToLower(rand.Text())
	provider.Register(kind, func(provider.Config) (provider.Provider, error) { return p, nil })
	writeTestFile(t, config.UserConfigPath(), fmt.Sprintf(`default_model = "fixture"
[agent]
collaboration = "isolated"
[codegraph]
enabled = false
[[providers]]
name = "fixture"
kind = %q
model = "fixture"
base_url = "http://fixture.invalid"
api_key_env = "ROOT_FIXTURE_KEY"
`, kind), 0600)
	if _, err := config.SetCredential("ROOT_FIXTURE_KEY", "fixture"); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	t.Chdir(root)
	for _, who := range []string{"AUTHOR", "COMMITTER"} {
		t.Setenv("GIT_"+who+"_NAME", "test")
		t.Setenv("GIT_"+who+"_EMAIL", "test@localhost")
	}
	for _, args := range [][]string{{"init", "-q"}, {"config", "core.autocrlf", "false"}, {"commit", "--allow-empty", "-qm", "base"}} {
		if out, err := gitcmd.Command(t.Context(), root, args...).CombinedOutput(); err != nil {
			t.Fatalf("git: %v %s", err, out)
		}
	}
	approveWorkspace(t, root)
	return root
}

func TestRunPublishesRootEnvironmentBeforeExecutingPrompt(t *testing.T) {
	root := rootRunFixture(t)
	var code int
	output, stderr := captureCLIOutput(t, func() { code = Run([]string{"run", "--yolo", "--output-format", "json", "write a file"}, "test") })
	if code != 0 {
		t.Fatalf("run exit %d: %s\n%s", code, output, stderr)
	}
	if _, err := os.Stat(filepath.Join(root, "from-run.txt")); !os.IsNotExist(err) {
		t.Fatal("CLI wrote source", err)
	}
	sessions, err := filepath.Glob(filepath.Join(resolveCLISessionDirFor(root), "*.jsonl"))
	sessions = slices.DeleteFunc(sessions, func(path string) bool { return !store.IsSessionTranscriptName(filepath.Base(path)) })
	if err != nil || len(sessions) != 1 {
		t.Fatalf("sessions = %v, %v", sessions, err)
	}
	binding, exists, err := sessionstore.LoadExecutionEnvironment(sessions[0])
	if err != nil || !exists {
		t.Fatalf("missing root binding: %v %v", exists, err)
	}
	if raw, err := os.ReadFile(filepath.Join(binding.WorkspaceRoot, "from-run.txt")); err != nil || string(raw) != "isolated\n" {
		t.Fatalf("candidate output = %q %v; %s", raw, err, output)
	}
}
