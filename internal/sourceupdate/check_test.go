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
