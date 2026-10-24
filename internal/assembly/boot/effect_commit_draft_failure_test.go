package boot

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"

	"reasonix/internal/contract/provider"
	"reasonix/internal/session/control"
)

type commitDraftFailsProvider struct{}

func (commitDraftFailsProvider) Name() string { return "commit-draft-fails" }

func (commitDraftFailsProvider) Stream(_ context.Context, req provider.Request) (<-chan provider.Chunk, error) {
	if len(req.Tools) == 0 && strings.Contains(req.Messages[0].Content, "commit message") {
		return nil, errors.New("upstream exploded")
	}
	ch := make(chan provider.Chunk, 3)
	ch <- provider.Chunk{Type: provider.ChunkText, Text: "ok"}
	ch <- provider.Chunk{Type: provider.ChunkUsage, Usage: &provider.Usage{PromptTokens: 10, CompletionTokens: 1}}
	ch <- provider.Chunk{Type: provider.ChunkDone}
	close(ch)
	return ch, nil
}

// A model that cannot draft must not strand the person: the proposal still
// names the staged set and its fingerprint, says why there is no message, and a
// message they write themselves commits exactly that set.
func TestEffectCommitDraftFailureStillOffersTheStagedSetForAManualMessage(t *testing.T) {
	isolateConfigHome(t)
	dir := robustTempDir(t)
	isolateGitGlobalConfig(t)
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

	provider.Register("commit-draft-fails", func(provider.Config) (provider.Provider, error) { return commitDraftFailsProvider{}, nil })
	writeFile(t, dir, "reasonix.toml", `
default_model = "test-model"

[agent]
system_prompt = "BASE"

[[providers]]
name = "test-model"
kind = "commit-draft-fails"
model = "x"
`)
	approveWorkspace(t, dir)
	ctrl, err := Build(context.Background(), Options{})
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	defer ctrl.Close()

	writeFile(t, dir, "x.go", "package x\n")
	git("add", "x.go")
	prop, err := ctrl.ProposeCommit(context.Background(), "")
	if err != nil {
		t.Fatalf("ProposeCommit = %v; a failed draft is a proposal with no message, not a refusal", err)
	}
	if prop.Message != "" || prop.DraftFailure != control.CommitDraftModelFailed {
		t.Fatalf("message %q, draftFailure %q; want no message and %q", prop.Message, prop.DraftFailure, control.CommitDraftModelFailed)
	}
	if len(prop.Files) != 1 || prop.Files[0].Path != "x.go" || prop.Fingerprint == "" {
		t.Fatalf("the staged set did not come back: %+v", prop)
	}
	if _, err := ctrl.CommitStaged(context.Background(), control.CommitRequest{Message: "", Fingerprint: prop.Fingerprint}); err == nil {
		t.Fatal("an empty message committed")
	}
	if _, err := ctrl.CommitStaged(context.Background(), control.CommitRequest{Message: "feat(x): add x by hand", Fingerprint: "stale"}); err == nil {
		t.Fatal("a stale fingerprint committed: the manual path must keep the guard")
	}
	res, err := ctrl.CommitStaged(context.Background(), control.CommitRequest{Message: "feat(x): add x by hand", Fingerprint: prop.Fingerprint})
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.TrimSpace(git("log", "-1", "--format=%s")); got != "feat(x): add x by hand" || res.Subject != got {
		t.Fatalf("recorded %q / %q", got, res.Subject)
	}
}
