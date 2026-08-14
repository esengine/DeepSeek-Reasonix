package cli

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"reasonix/internal/sourceupdate"
)

func TestSourceUpdateCommandRequiresExplicitCheck(t *testing.T) {
	stderr := captureStderr(t, func() {
		if code := sourceUpdateCommand([]string{"--root", "D:/src"}); code != 2 {
			t.Fatalf("exit code = %d, want 2", code)
		}
	})
	if !strings.Contains(stderr, "--check") {
		t.Fatalf("stderr = %q, want explicit --check guidance", stderr)
	}
}

func TestSourceUpdateCommandJSONUsesProvidedRoot(t *testing.T) {
	previous := checkSourceUpdate
	t.Cleanup(func() { checkSourceUpdate = previous })
	var gotRoot string
	checkSourceUpdate = func(_ context.Context, root string) (sourceupdate.Result, error) {
		gotRoot = root
		return sourceupdate.Result{
			SourceRoot:      root,
			Branch:          "codex/contrib",
			Head:            strings.Repeat("c", 40),
			LocalBase:       strings.Repeat("a", 40),
			RemoteURL:       sourceupdate.DefaultRemoteURL,
			RemoteRef:       sourceupdate.DefaultRemoteRef,
			RemoteHead:      strings.Repeat("b", 40),
			Status:          sourceupdate.StatusUpstreamUpdate,
			HasLocalPatches: true,
		}, nil
	}

	stdout, stderr := captureCLIOutput(t, func() {
		if code := sourceUpdateCommand([]string{"--check", "--root", "D:/src", "--json"}); code != 0 {
			t.Fatalf("exit code = %d, want 0", code)
		}
	})
	if gotRoot != "D:/src" {
		t.Fatalf("root = %q, want D:/src", gotRoot)
	}
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty", stderr)
	}
	var result sourceupdate.Result
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("decode JSON %q: %v", stdout, err)
	}
	if result.Status != sourceupdate.StatusUpstreamUpdate || !result.HasLocalPatches {
		t.Fatalf("result = %+v", result)
	}
}

func TestSourceUpdateCommandReturnsJSONOnCheckFailure(t *testing.T) {
	previous := checkSourceUpdate
	t.Cleanup(func() { checkSourceUpdate = previous })
	checkSourceUpdate = func(context.Context, string) (sourceupdate.Result, error) {
		return sourceupdate.Result{Status: sourceupdate.StatusCheckFailed, Message: "remote unavailable"}, errors.New("remote unavailable")
	}

	stdout, stderr := captureCLIOutput(t, func() {
		if code := sourceUpdateCommand([]string{"--check", "--root", "D:/src", "--json"}); code != 1 {
			t.Fatalf("exit code = %d, want 1", code)
		}
	})
	if stderr != "" {
		t.Fatalf("stderr = %q, want empty for machine-readable output", stderr)
	}
	var result sourceupdate.Result
	if err := json.Unmarshal([]byte(stdout), &result); err != nil {
		t.Fatalf("decode JSON %q: %v", stdout, err)
	}
	if result.Status != sourceupdate.StatusCheckFailed {
		t.Fatalf("status = %q, want %q", result.Status, sourceupdate.StatusCheckFailed)
	}
}
