package boot

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

// A slow MCP connect (spawning the server subprocess) must run outside
// t.mu: the callback probes the lock and fails if Execute still holds it.
func TestConnectToolSourceMCPConnectRunsWithoutLock(t *testing.T) {
	tsc := &toolSourceConnector{}
	tsc.mcp = func(context.Context, string) (string, error) {
		free := make(chan struct{})
		go func() {
			tsc.mu.Lock()
			tsc.mu.Unlock() //nolint:staticcheck // probe: lock must be immediately acquirable
			close(free)
		}()
		select {
		case <-free:
		case <-time.After(500 * time.Millisecond):
			t.Error("t.mu still held while the MCP connect callback was running")
		}
		return `enabled MCP server "srv" tools: mcp__srv__x.`, nil
	}

	out, err := tsc.Execute(context.Background(), json.RawMessage(`{"source":"mcp","name":"srv"}`))
	if err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	if want := `enabled MCP server "srv" tools: mcp__srv__x.`; out != want {
		t.Fatalf("Execute output = %q, want %q", out, want)
	}
}

// A connect_tool_source call for a fast source (web_fetch) must not queue
// behind a concurrent MCP connect that is stuck spawning its server.
func TestConnectToolSourceSlowMCPDoesNotBlockFastSource(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	tsc := &toolSourceConnector{
		webFetch: func(context.Context) (string, error) { return "enabled web_fetch.", nil },
		mcp: func(context.Context, string) (string, error) {
			close(started)
			<-release
			return `enabled MCP server "slow" tools: mcp__slow__x.`, nil
		},
		mcpNames: []string{"slow"},
	}

	slowDone := make(chan error, 1)
	go func() {
		_, err := tsc.Execute(context.Background(), json.RawMessage(`{"source":"mcp","name":"slow"}`))
		slowDone <- err
	}()

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("slow MCP connect callback never started")
	}

	fastDone := make(chan struct{})
	go func() {
		defer close(fastDone)
		out, err := tsc.Execute(context.Background(), json.RawMessage(`{"source":"web_fetch"}`))
		if err != nil {
			t.Errorf("web_fetch Execute error: %v", err)
			return
		}
		if out != "enabled web_fetch." {
			t.Errorf("web_fetch Execute output = %q, want %q", out, "enabled web_fetch.")
		}
	}()

	select {
	case <-fastDone:
	case <-time.After(time.Second):
		t.Fatal("web_fetch connect blocked behind an in-flight MCP connect")
	}

	close(release)
	if err := <-slowDone; err != nil {
		t.Fatalf("slow MCP Execute error: %v", err)
	}
}

// The fast MCP paths (listing servers, missing callback) keep their existing
// behavior and still run under the lock.
func TestConnectToolSourceMCPFastPathsUnchanged(t *testing.T) {
	tsc := &toolSourceConnector{mcpNames: []string{"b", "a"}}
	out, err := tsc.Execute(context.Background(), json.RawMessage(`{"source":"mcp"}`))
	if err != nil {
		t.Fatalf("list Execute error: %v", err)
	}
	want := `Configured MCP servers: a, b. Call connect_tool_source again with source="mcp" and name set to connect one server.`
	if out != want {
		t.Fatalf("list output = %q, want %q", out, want)
	}

	if _, err := tsc.Execute(context.Background(), json.RawMessage(`{"source":"mcp","name":"a"}`)); err == nil {
		t.Fatal("expected error when MCP callback is unavailable")
	} else if err.Error() != "MCP source is unavailable in this session" {
		t.Fatalf("unavailable error = %q", err.Error())
	}
}
