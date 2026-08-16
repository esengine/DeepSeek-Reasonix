package sourceupdate

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type fakeGitRunner struct {
	outputs map[string][]byte
	errors  map[string]error
	calls   []string
}

func (f *fakeGitRunner) Run(_ context.Context, _ string, args ...string) ([]byte, error) {
	key := strings.Join(args, " ")
	f.calls = append(f.calls, key)
	if err := f.errors[key]; err != nil {
		return nil, err
	}
	return f.outputs[key], nil
}

func sourceUpdateRunner(localBase, remoteHead string) *fakeGitRunner {
	return &fakeGitRunner{
		outputs: map[string][]byte{
			"rev-parse --show-toplevel":                          []byte("D:/src\n"),
			"branch --show-current":                              []byte("codex/contrib\n"),
			"rev-parse HEAD":                                     []byte(strings.Repeat("c", 40) + "\n"),
			"rev-parse refs/remotes/origin/main-v2":              []byte(localBase + "\n"),
			"ls-remote --heads " + DefaultRemoteURL + " main-v2": []byte(remoteHead + "\trefs/heads/main-v2\n"),
		},
		errors: map[string]error{},
	}
}

func TestCheckReportsUpstreamUpdateWithoutFetching(t *testing.T) {
	localBase := strings.Repeat("a", 40)
	remoteHead := strings.Repeat("b", 40)
	runner := sourceUpdateRunner(localBase, remoteHead)

	result, err := CheckWithRunner(context.Background(), "D:/src", runner)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != StatusUpstreamUpdate {
		t.Fatalf("status = %q, want %q", result.Status, StatusUpstreamUpdate)
	}
	if result.LocalBase != localBase || result.RemoteHead != remoteHead {
		t.Fatalf("result = %+v, want local base %q and remote head %q", result, localBase, remoteHead)
	}
	if !result.HasLocalPatches {
		t.Fatal("custom commits should be reported separately from upstream status")
	}
	for _, call := range runner.calls {
		if strings.HasPrefix(call, "fetch ") || strings.Contains(call, " fetch ") {
			t.Fatalf("source check must not fetch: %q", call)
		}
	}
}

func TestCheckReportsUpToDateWhenLocalBaseMatchesRemote(t *testing.T) {
	base := strings.Repeat("d", 40)
	runner := sourceUpdateRunner(base, base)

	result, err := CheckWithRunner(context.Background(), "D:/src", runner)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != StatusUpToDate {
		t.Fatalf("status = %q, want %q", result.Status, StatusUpToDate)
	}
	if !result.HasLocalPatches {
		t.Fatal("the local contribution HEAD should remain visible")
	}
}

func TestCheckReportsMissingLocalBaselineWithoutCallingRemote(t *testing.T) {
	runner := sourceUpdateRunner(strings.Repeat("a", 40), strings.Repeat("b", 40))
	runner.errors["rev-parse refs/remotes/origin/main-v2"] = errors.New("unknown revision")

	result, err := CheckWithRunner(context.Background(), "D:/src", runner)
	if err == nil {
		t.Fatal("missing local baseline should fail explicitly")
	}
	if result.Status != StatusBaselineMissing {
		t.Fatalf("status = %q, want %q", result.Status, StatusBaselineMissing)
	}
	for _, call := range runner.calls {
		if strings.HasPrefix(call, "ls-remote ") {
			t.Fatal("remote check must not run when the local baseline is missing")
		}
	}
}

func TestCheckRejectsInvalidRemoteHead(t *testing.T) {
	runner := sourceUpdateRunner(strings.Repeat("a", 40), "not-a-git-hash")

	result, err := CheckWithRunner(context.Background(), "D:/src", runner)
	if err == nil {
		t.Fatal("invalid remote head should fail")
	}
	if result.Status != StatusCheckFailed {
		t.Fatalf("status = %q, want %q", result.Status, StatusCheckFailed)
	}
}

func TestCheckIncludesFailureReasonInResult(t *testing.T) {
	runner := sourceUpdateRunner(strings.Repeat("a", 40), strings.Repeat("b", 40))
	runner.errors["ls-remote --heads "+DefaultRemoteURL+" main-v2"] = errors.New("network unavailable")

	result, err := CheckWithRunner(context.Background(), "D:/src", runner)
	if err == nil {
		t.Fatal("remote failure should be returned")
	}
	if !strings.Contains(result.Message, "network unavailable") {
		t.Fatalf("message = %q, want the underlying failure reason", result.Message)
	}
}

func TestFetchUpdatesTrackingRefWithoutChangingWorktree(t *testing.T) {
	remoteHead := strings.Repeat("b", 40)
	runner := &fakeGitRunner{
		outputs: map[string][]byte{
			"rev-parse --show-toplevel":                []byte("D:/src\n"),
			"branch --show-current":                    []byte("codex/contrib\n"),
			"rev-parse HEAD":                           []byte(strings.Repeat("c", 40) + "\n"),
			"remote get-url origin":                    []byte(DefaultRemoteURL + "\n"),
			"status --porcelain --untracked-files=all": nil,
			"fetch --no-tags origin refs/heads/main-v2:refs/remotes/origin/main-v2": nil,
			"rev-parse refs/remotes/origin/main-v2":                                 []byte(remoteHead + "\n"),
		},
		errors: map[string]error{},
	}

	result, err := FetchWithRunner(context.Background(), "D:/src", runner)
	if err != nil {
		t.Fatal(err)
	}
	if result.Status != StatusUpToDate {
		t.Fatalf("status = %q, want %q", result.Status, StatusUpToDate)
	}
	if result.LocalBase != remoteHead || !result.HasLocalPatches {
		t.Fatalf("result = %+v, want fetched base %q and preserved local patches", result, remoteHead)
	}
	for _, call := range runner.calls {
		if strings.Contains(call, "checkout") || strings.Contains(call, "reset") || strings.Contains(call, "merge") || strings.Contains(call, "rebase") {
			t.Fatalf("fetch must not change the worktree: %q", call)
		}
	}
}

func TestFetchRejectsDirtyWorktreeBeforeFetching(t *testing.T) {
	runner := &fakeGitRunner{
		outputs: map[string][]byte{
			"rev-parse --show-toplevel":                []byte("D:/src\n"),
			"branch --show-current":                    []byte("codex/contrib\n"),
			"rev-parse HEAD":                           []byte(strings.Repeat("c", 40) + "\n"),
			"remote get-url origin":                    []byte(DefaultRemoteURL + "\n"),
			"status --porcelain --untracked-files=all": []byte(" M internal/sourceupdate/check.go\n"),
		},
		errors: map[string]error{},
	}

	result, err := FetchWithRunner(context.Background(), "D:/src", runner)
	if err == nil {
		t.Fatal("dirty worktree should reject fetch")
	}
	if result.Status != StatusCheckFailed {
		t.Fatalf("status = %q, want %q", result.Status, StatusCheckFailed)
	}
	if !strings.Contains(result.Message, "clean") {
		t.Fatalf("message = %q, want clean-worktree guidance", result.Message)
	}
	for _, call := range runner.calls {
		if strings.HasPrefix(call, "fetch ") {
			t.Fatalf("dirty worktree must not fetch: %q", call)
		}
	}
}

func TestFetchRejectsUnexpectedOriginWithoutEchoingURL(t *testing.T) {
	secretURL := "https://user:secret@example.invalid/reasonix.git"
	runner := &fakeGitRunner{
		outputs: map[string][]byte{
			"rev-parse --show-toplevel": []byte("D:/src\n"),
			"branch --show-current":     []byte("codex/contrib\n"),
			"rev-parse HEAD":            []byte(strings.Repeat("c", 40) + "\n"),
			"remote get-url origin":     []byte(secretURL + "\n"),
		},
		errors: map[string]error{},
	}

	result, err := FetchWithRunner(context.Background(), "D:/src", runner)
	if err == nil {
		t.Fatal("unexpected origin should reject fetch")
	}
	if strings.Contains(result.Message, secretURL) || strings.Contains(err.Error(), secretURL) {
		t.Fatalf("unexpected origin URL leaked in error: result=%q err=%q", result.Message, err)
	}
	for _, call := range runner.calls {
		if strings.HasPrefix(call, "fetch ") {
			t.Fatalf("unexpected origin must not fetch: %q", call)
		}
	}
}
