package boot

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"

	"reasonix/internal/contract/event"
	"reasonix/internal/contract/provider"
	"reasonix/internal/platform/gitcommit"
	"reasonix/internal/session/control"
)

type commitEffectProvider struct {
	mu   sync.Mutex
	reqs []provider.Request
}

func (p *commitEffectProvider) Name() string { return "commit-effect" }

func (p *commitEffectProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	p.mu.Lock()
	p.reqs = append(p.reqs, req)
	p.mu.Unlock()
	text := "ok"
	if len(req.Tools) == 0 && strings.Contains(req.Messages[0].Content, "commit message") {
		text = "feat(x): add x"
	}
	ch := make(chan provider.Chunk, 3)
	ch <- provider.Chunk{Type: provider.ChunkText, Text: text}
	ch <- provider.Chunk{Type: provider.ChunkUsage, Usage: &provider.Usage{PromptTokens: 100, CompletionTokens: 7}}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

func (p *commitEffectProvider) split() (agent, commit []provider.Request) {
	p.mu.Lock()
	defer p.mu.Unlock()
	for _, r := range p.reqs {
		if len(r.Tools) > 0 {
			agent = append(agent, r)
		} else {
			commit = append(commit, r)
		}
	}
	return
}

// A commit message is drafted by a request of its own: it reads the staged diff
// and nothing of the session, leaves the executor's cache-stable prefix as it
// was, is billed under its own source, and commits only once confirmed.
func TestEffectCommitMessageRidesItsOwnRequestAndCommitsOnConfirm(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	for _, k := range []string{"GIT_AUTHOR", "GIT_COMMITTER"} {
		t.Setenv(k+"_NAME", "t")
		t.Setenv(k+"_EMAIL", "t@t")
	}
	git := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
		return string(out)
	}
	git("init", "-q")
	writeFile(t, dir, "base.go", "package a\n")
	git("add", "base.go")
	git("commit", "-qm", "base")
	t.Chdir(dir)

	prov := &commitEffectProvider{}
	provider.Register("commit-effect", func(provider.Config) (provider.Provider, error) { return prov, nil })
	writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"

[agent]
system_prompt = "BASE"

[[providers]]
name = "test-model"
kind = "commit-effect"
model = "x"
`)
	approveWorkspace(t, dir)

	var mu sync.Mutex
	var usage []event.Event
	sink := event.FuncSink(func(e event.Event) {
		if e.Kind == event.Usage {
			mu.Lock()
			usage = append(usage, e)
			mu.Unlock()
		}
	})
	ctrl, err := Build(context.Background(), Options{Sink: sink})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()

	if err := ctrl.Run(context.Background(), "first turn SESSION-MARKER"); err != nil {
		t.Fatal(err)
	}
	writeFile(t, dir, "x.go", "package x\n")
	writeFile(t, dir, ".env", "TOKEN=hunter2-plain\n")
	git("add", "x.go", ".env")

	prop, err := ctrl.ProposeCommit(context.Background())
	if err != nil {
		t.Fatalf("ProposeCommit: %v", err)
	}
	if prop.Message != "feat(x): add x" || len(prop.Files) != 2 {
		t.Fatalf("proposal = %+v", prop)
	}
	if err := ctrl.Run(context.Background(), "second turn"); err != nil {
		t.Fatal(err)
	}

	agentReqs, commitReqs := prov.split()
	if len(commitReqs) != 1 {
		t.Fatalf("%d commit requests, want 1", len(commitReqs))
	}
	wire := commitReqs[0].Messages[0].Content + commitReqs[0].Messages[1].Content
	if strings.Contains(wire, "SESSION-MARKER") || strings.Contains(wire, "hunter2") || !strings.Contains(wire, "package x") {
		t.Fatalf("the commit request must carry the staged diff only, minus secret files:\n%s", wire)
	}
	first, last := agentReqs[0], agentReqs[len(agentReqs)-1]
	if len(last.Messages) < len(first.Messages) || !reflect.DeepEqual(last.Messages[:len(first.Messages)], first.Messages) || !reflect.DeepEqual(first.Tools, last.Tools) {
		t.Fatal("drafting a commit message moved the executor's cache-stable prefix")
	}
	mu.Lock()
	billed := 0
	for _, u := range usage {
		if u.UsageSource == event.UsageSourceCommitMessage {
			billed++
		}
	}
	mu.Unlock()
	if billed != 1 {
		t.Fatalf("%d commit-message usage events reached the frontend sink, want 1", billed)
	}
	if got := strings.TrimSpace(git("rev-list", "--count", "HEAD")); got != "1" {
		t.Fatalf("a proposal committed something: %s commits", got)
	}

	req := func(ack bool) control.CommitRequest {
		return control.CommitRequest{Message: prop.Message, Fingerprint: prop.Fingerprint, AcknowledgeSecrets: ack}
	}
	if _, err := ctrl.CommitStaged(context.Background(), req(false)); !errors.Is(err, gitcommit.ErrSensitiveStaged) {
		t.Fatalf("an unacknowledged secret file was committed: %v", err)
	}
	res, err := ctrl.CommitStaged(context.Background(), req(true))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(git("log", "-1", "--format=%s")); got != "feat(x): add x" || res.Subject != got {
		t.Fatalf("recorded %q / %q", got, res.Subject)
	}
	if _, err := os.Stat(filepath.Join(dir, ".git", "HEAD")); err != nil {
		t.Fatal(err)
	}
}
