package rootenv

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/base/filelock"
	"reasonix/internal/platform/gitcmd"
	"reasonix/internal/state/mutation"
)

func fixture(t *testing.T) (*Store, string) {
	t.Helper()
	root := t.TempDir()
	git(t, root, "init", "-q")
	git(t, root, "config", "core.autocrlf", "false")
	write(t, filepath.Join(root, "a"), "base\n")
	git(t, root, "add", "-A")
	git(t, root, "commit", "-qm", "base")
	repo, err := gitcmd.Open(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	coord, err := mutation.New(root, t.TempDir(), nil)
	if err != nil {
		t.Fatal(err)
	}
	s, err := New(t.TempDir(), root, repo, coord)
	if err != nil {
		t.Fatal(err)
	}
	return s, root
}

func git(t *testing.T, root string, args ...string) string {
	t.Helper()
	cmd := gitcmd.Command(t.Context(), root, args...)
	cmd.Env = append(cmd.Env, "GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@localhost", "GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@localhost")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func write(t *testing.T, path, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestPersistentEnvironmentRetainsDirtySnapshotAndSurvivesClose(t *testing.T) {
	s, source := fixture(t)
	write(t, filepath.Join(source, "a"), "dirty\n")
	write(t, filepath.Join(source, "new"), "untracked\n")
	index := git(t, source, "diff", "--cached", "--raw")
	e, err := s.Prepare(t.Context(), "session", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	r := e.Record()
	for name, want := range map[string]string{"a": "dirty\n", "new": "untracked\n"} {
		got, err := os.ReadFile(filepath.Join(r.Candidate.WorktreeRoot, name))
		if err != nil || string(got) != want {
			t.Fatalf("%s = %q, %v", name, got, err)
		}
	}
	write(t, filepath.Join(r.Candidate.WorktreeRoot, "a"), "next turn\n")
	if _, err := s.Open(t.Context(), r.ID, "session"); !errors.Is(err, filelock.ErrHeld) {
		t.Fatalf("concurrent open: %v", err)
	}
	if err := e.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(r.Candidate.WorktreeRoot); err != nil {
		t.Fatal("close removed live worktree", err)
	}
	reopened, err := s.Open(t.Context(), r.ID, "session")
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if reopened.Record().RunGeneration == r.RunGeneration {
		t.Fatal("restart reused execution generation")
	}
	got, _ := os.ReadFile(filepath.Join(reopened.Record().Candidate.WorktreeRoot, "a"))
	if string(got) != "next turn\n" {
		t.Fatalf("reopen reset work: %q", got)
	}
	if git(t, source, "diff", "--cached", "--raw") != index {
		t.Fatal("source index changed")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(r.Candidate.WorktreeRoot), "entry-v2.json")); !os.IsNotExist(err) {
		t.Fatal("root environment became disposable iso result")
	}
	if _, err := s.Open(t.Context(), r.ID, "other-session"); !errors.Is(err, ErrOwner) {
		t.Fatalf("wrong owner: %v", err)
	}
}

func TestInterruptedPreparationAndUnknownRecordAreRetained(t *testing.T) {
	s, _ := fixture(t)
	s.preparationProbe = func(r Record) error {
		if r.State == "materializing" {
			return context.Canceled
		}
		return nil
	}
	e, err := s.Prepare(t.Context(), "session", nil, nil)
	if !errors.Is(err, context.Canceled) || e == nil {
		t.Fatalf("prepare = %v, %v", e, err)
	}
	r := e.Record()
	if _, err := s.Open(t.Context(), r.ID, "session"); !errors.Is(err, ErrRecovery) {
		t.Fatalf("interrupted: %v", err)
	}
	path := s.recordPath(r.ID)
	write(t, path, `{"version":999,"future":"keep"}`)
	if _, err := s.Open(t.Context(), r.ID, "session"); !errors.Is(err, ErrProtocol) {
		t.Fatalf("future version: %v", err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != `{"version":999,"future":"keep"}` {
		t.Fatal("unknown record rewritten")
	}
}

func TestOldHandleCannotMutateOrReleaseReopenedEnvironment(t *testing.T) {
	s, _ := fixture(t)
	e, err := s.Prepare(t.Context(), "session", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	r := e.Record()
	if err := e.Close(); err != nil {
		t.Fatal(err)
	}
	next, err := s.Open(t.Context(), r.ID, "session")
	if err != nil {
		t.Fatal(err)
	}
	defer next.Close()
	if err := e.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Open(t.Context(), r.ID, "session"); !errors.Is(err, filelock.ErrHeld) {
		t.Fatalf("old close released successor: %v", err)
	}
}

func TestReopenRejectsWorkspaceEscapeFromSelectedSubdirectory(t *testing.T) {
	s, source := fixture(t)
	sub := filepath.Join(source, "sub")
	if err := os.Mkdir(sub, 0700); err != nil {
		t.Fatal(err)
	}
	write(t, filepath.Join(sub, "a"), "selected\n")
	git(t, source, "add", "-A")
	git(t, source, "commit", "-qm", "subdirectory")
	repo, err := gitcmd.Open(t.Context(), sub)
	if err != nil {
		t.Fatal(err)
	}
	substore, err := New(filepath.Dir(s.dir), sub, repo, s.mutations)
	if err != nil {
		t.Fatal(err)
	}
	e, err := substore.Prepare(t.Context(), "session", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	r := e.Record()
	if err := e.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(r.Candidate.WorkspaceRoot); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(sub, r.Candidate.WorkspaceRoot); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	opened, err := substore.Open(t.Context(), r.ID, "session")
	if opened != nil {
		opened.Close()
	}
	if !errors.Is(err, ErrRecovery) {
		t.Fatalf("symlink escaped to source: %v", err)
	}
	if err := os.Remove(r.Candidate.WorkspaceRoot); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(r.Candidate.WorkspaceRoot, 0700); err != nil {
		t.Fatal(err)
	}
	r.Candidate.WorkspaceRoot = source
	raw, _ := json.Marshal(r)
	if err := os.WriteFile(substore.recordPath(r.ID), raw, 0600); err != nil {
		t.Fatal(err)
	}
	opened, err = substore.Open(t.Context(), r.ID, "session")
	if opened != nil {
		opened.Close()
	}
	if !errors.Is(err, ErrRecovery) {
		t.Fatalf("record cwd escaped to source: %v", err)
	}
}

func TestCancelledReopenDoesNotAdvanceGenerationOrKeepOwnership(t *testing.T) {
	s, _ := fixture(t)
	e, err := s.Prepare(t.Context(), "session", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	r := e.Record()
	e.Close()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	opened, err := s.Open(ctx, r.ID, "session")
	if opened != nil {
		opened.Close()
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled open = %v", err)
	}
	after, err := s.load(r.ID)
	if err != nil || after.RunGeneration != r.RunGeneration {
		t.Fatalf("cancel advanced generation: %+v %v", after, err)
	}
	opened, err = s.Open(t.Context(), r.ID, "session")
	if err != nil {
		t.Fatal("cancel retained ownership:", err)
	}
	opened.Close()
}

func TestReopenSourceThroughCanonicalAliasPreservesUnknownFields(t *testing.T) {
	s, source := fixture(t)
	alias := filepath.Join(t.TempDir(), "source")
	if err := os.Symlink(filepath.Dir(source), alias); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	alias = filepath.Join(alias, filepath.Base(source))
	repo, err := gitcmd.Open(t.Context(), alias)
	if err != nil {
		t.Fatal(err)
	}
	aliased, err := New(filepath.Dir(s.dir), alias, repo, s.mutations)
	if err != nil {
		t.Fatal(err)
	}
	e, err := aliased.Prepare(t.Context(), "session", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	r := e.Record()
	e.Close()
	raw, err := os.ReadFile(s.recordPath(r.ID))
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	fields["future"] = json.RawMessage(`{"keep":true}`)
	var candidate map[string]json.RawMessage
	if err := json.Unmarshal(fields["candidate"], &candidate); err != nil {
		t.Fatal(err)
	}
	candidate["future"] = json.RawMessage(`{"nested":true}`)
	fields["candidate"], _ = json.Marshal(candidate)
	raw, _ = json.Marshal(fields)
	if err := os.WriteFile(s.recordPath(r.ID), raw, 0600); err != nil {
		t.Fatal(err)
	}
	opened, err := s.Open(t.Context(), r.ID, "session")
	if err != nil {
		t.Fatal("canonical source could not reopen:", err)
	}
	opened.Close()
	after, _ := os.ReadFile(s.recordPath(r.ID))
	if !strings.Contains(string(after), `"future":{"keep":true}`) || !strings.Contains(string(after), `"future":{"nested":true}`) {
		t.Fatalf("unknown fields dropped: %s", after)
	}
}

func TestRebuildRetainsIndependentEnvironmentOwnership(t *testing.T) {
	s, _ := fixture(t)
	e, err := s.Prepare(t.Context(), "session", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	r := e.Record()
	next, err := e.Retain()
	if err != nil {
		t.Fatal(err)
	}
	if err := e.Close(); err != nil {
		t.Fatal(err)
	}
	if err := e.Validate(t.Context()); !errors.Is(err, ErrStale) {
		t.Fatalf("closed handle valid: %v", err)
	}
	if _, err := e.Retain(); !errors.Is(err, ErrStale) {
		t.Fatalf("closed handle retained: %v", err)
	}
	e.Close()
	if _, err := s.Open(t.Context(), r.ID, "session"); !errors.Is(err, filelock.ErrHeld) {
		t.Fatalf("rebuild lost ownership: %v", err)
	}
	if err := next.Validate(t.Context()); err != nil {
		t.Fatal(err)
	}
	next.Close()
	opened, err := s.Open(t.Context(), r.ID, "session")
	if err != nil {
		t.Fatal(err)
	}
	opened.Close()
}
