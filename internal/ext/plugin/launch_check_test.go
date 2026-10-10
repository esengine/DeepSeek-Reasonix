package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
)

// A spec's launch check runs before any process or connection: a refusal
// starts nothing, reports an approval the user owes rather than a broken
// server, and keeps the cause the check gave.
func TestLaunchCheckRefusalStartsNothing(t *testing.T) {
	redirectCache(t)
	startCount := filepath.Join(testenv.TempDir(t), "starts")
	cause := errors.New("declaration changed")
	refuse := true
	spec := Spec{
		Name: "project-server", Command: os.Args[0], Args: []string{"-test.run=TestHelperProcess", "--"},
		Env: map[string]string{
			"GO_WANT_HELPER_PROCESS":     "1",
			"GO_WANT_HELPER_START_COUNT": startCount,
		},
		ConfigSource: "project_mcp_json", Authorized: true,
		LaunchCheck: func() error {
			if refuse {
				return cause
			}
			return nil
		},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, _, err := StartAll(ctx, []Spec{spec})
	if err == nil || !errors.Is(err, cause) || !requiresLaunchApproval(err) {
		t.Fatalf("refused start error = %v; want the check's cause, reported as an approval owed", err)
	}
	if !strings.Contains(err.Error(), "changed") {
		t.Fatalf("refusal does not say the server changed: %v", err)
	}
	if got := readHelperCounter(t, startCount); got != 0 {
		t.Fatalf("refused launch started the server %d times", got)
	}

	refuse = false
	host, tools, err := StartAll(ctx, []Spec{spec})
	if err != nil || len(tools) == 0 {
		t.Fatalf("passing check: %v (%d tools)", err, len(tools))
	}
	host.Close()
	if got := readHelperCounter(t, startCount); got != 1 {
		t.Fatalf("starts after the check passed = %d, want 1", got)
	}
}

// A live child that dies is replaced by redial; the replacement must pass the
// same launch check, so a declaration changed while the first child ran is
// refused rather than restarted.
func TestRedialRunsLaunchCheck(t *testing.T) {
	redirectCache(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	startCount := filepath.Join(testenv.TempDir(t), "starts")
	var refuse atomic.Bool
	cause := errors.New("declaration changed")
	spec := Spec{
		Name: "project-server", Command: os.Args[0], Args: []string{"-test.run=TestHelperProcess", "--"},
		Env: map[string]string{
			"GO_WANT_HELPER_PROCESS":     "1",
			"GO_WANT_HELPER_START_COUNT": startCount,
			"GO_WANT_HELPER_DIE_ON_CALL": "1",
		},
		ConfigSource: "project_mcp_json", Authorized: true,
		LaunchCheck: func() error {
			if refuse.Load() {
				return cause
			}
			return nil
		},
	}
	host, tools, err := StartAll(ctx, []Spec{spec})
	if err != nil {
		t.Fatal(err)
	}
	defer host.Close()
	var echo interface {
		Execute(context.Context, json.RawMessage) (string, error)
	}
	for _, c := range tools {
		if strings.HasSuffix(c.Name(), "__echo") {
			echo = c
		}
	}
	if echo == nil {
		t.Fatal("no echo tool")
	}
	if _, err := echo.Execute(ctx, json.RawMessage(`{"msg":"kill"}`)); err == nil {
		t.Fatal("the dying call should fail")
	}
	refuse.Store(true)
	_, err = echo.Execute(ctx, json.RawMessage(`{"msg":"after"}`))
	if err == nil || !errors.Is(err, cause) {
		t.Errorf("redial did not refuse with the check's cause: %v", err)
	}
	if got := readHelperCounter(t, startCount); got != 1 {
		t.Errorf("redial started the changed server (%d starts)", got)
	}
}

// Two spellings of one variable in a declaration must merge the same way on
// every start; map order deciding it would let the launcher run from a
// directory the approval did not look at.
func TestMergeEnvIsDeterministic(t *testing.T) {
	overrides := map[string]string{"PATH": "/a", "Path": "/b", "Z": "1", "A": "2", "path": "/c"}
	first := strings.Join(mergeEnv([]string{"HOME=/h"}, overrides), "\n")
	for range 64 {
		if got := strings.Join(mergeEnv([]string{"HOME=/h"}, overrides), "\n"); got != first {
			t.Fatalf("mergeEnv order changed between runs:\n%s\n---\n%s", first, got)
		}
	}
}
