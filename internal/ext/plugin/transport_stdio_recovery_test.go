package plugin

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"reasonix/internal/base/testenv"
	"reasonix/internal/contract/tool"
)

// A stdio child that dies after a healthy handshake must not poison its server
// for the rest of the session. The transport latches the read error and the
// Host keeps the client, so without recovery every later call reports
// "read: EOF" with the tail of the startup stderr attached — which reads like a
// server that never started, and only a restart clears it.
func TestStdioServerRecoversAfterChildDies(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	startCount := filepath.Join(testenv.TempDir(t), "starts")
	const startupLine = "[DuckDB] connect local database: data/processed/duckdb_data.db"
	host, echo := startDyingHelperServer(t, ctx, startCount, startupLine)
	defer host.Close()

	// The call that kills the child fails: its request already reached the
	// server, so replaying it could repeat a side effect.
	_, err := echo.Execute(ctx, json.RawMessage(`{"msg":"first"}`))
	if err == nil {
		t.Fatal("the call that kills the child should fail")
	}
	if strings.Contains(err.Error(), startupLine) || !strings.Contains(err.Error(), "output omitted") {
		t.Fatalf("error should omit the child's output, got %v", err)
	}
	// The last words of a server that died quietly are its startup banner, so
	// the message has to say which of the two happened — a bare "read: EOF"
	// next to a connect line reads as a server that never came up.
	for _, want := range []string{"server exited while handling", "the next call starts a fresh one"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error should say the server left mid-call, got %v", err)
		}
	}

	// Every call after it must recover: the child is known dead before this
	// request is written, so reconnecting and dispatching repeats nothing.
	out, err := echo.Execute(ctx, json.RawMessage(`{"msg":"second"}`))
	if err != nil {
		t.Fatalf("call after the child died: %v", err)
	}
	if out != "echo: second" {
		t.Fatalf("result = %q, want %q", out, "echo: second")
	}
	if got := readHelperCounter(t, startCount); got != 2 {
		t.Fatalf("helper starts = %d, want 2 (one reconnect, not one per call)", got)
	}

	// A recovered server stays recovered rather than reconnecting per call.
	if out, err := echo.Execute(ctx, json.RawMessage(`{"msg":"third"}`)); err != nil || out != "echo: third" {
		t.Fatalf("third call = %q, %v", out, err)
	}
	if got := readHelperCounter(t, startCount); got != 2 {
		t.Fatalf("helper starts after a third call = %d, want 2", got)
	}
}

// Concurrent callers that all find the child dead share one reconnect instead
// of racing to spawn a process each.
func TestStdioReconnectIsSingleFlight(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	startCount := filepath.Join(testenv.TempDir(t), "starts")
	host, echo := startDyingHelperServer(t, ctx, startCount, "")
	defer host.Close()

	if _, err := echo.Execute(ctx, json.RawMessage(`{"msg":"kill"}`)); err == nil {
		t.Fatal("the call that kills the child should fail")
	}

	const callers = 8
	errs := make(chan error, callers)
	for range callers {
		go func() {
			_, err := echo.Execute(ctx, json.RawMessage(`{"msg":"x"}`))
			errs <- err
		}()
	}
	for range callers {
		if err := <-errs; err != nil {
			t.Fatalf("concurrent call after death: %v", err)
		}
	}
	if got := readHelperCounter(t, startCount); got != 2 {
		t.Fatalf("helper starts = %d, want 2 (%d callers shared one reconnect)", got, callers)
	}
}

func startDyingHelperServer(t *testing.T, ctx context.Context, startCount, startupStderr string) (*Host, tool.Tool) {
	t.Helper()
	spec := Spec{
		Name:    "data-analysis",
		Command: os.Args[0],
		Args:    []string{"-test.run=TestHelperProcess", "--"},
		Env: map[string]string{
			"GO_WANT_HELPER_PROCESS":        "1",
			"GO_WANT_HELPER_START_COUNT":    startCount,
			"GO_WANT_HELPER_STARTUP_STDERR": startupStderr,
			"GO_WANT_HELPER_DIE_ON_CALL":    "1",
		},
	}
	host, tools, err := StartAll(ctx, []Spec{spec})
	if err != nil {
		t.Fatalf("StartAll: %v", err)
	}
	for _, candidate := range tools {
		if strings.HasSuffix(candidate.Name(), "__echo") {
			return host, candidate
		}
	}
	host.Close()
	t.Fatalf("echo tool missing from %d tools", len(tools))
	return nil, nil
}
