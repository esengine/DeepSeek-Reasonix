package plugin

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"testing"
	"time"

	"reasonix/internal/tool"
)

type discardWriteCloser struct{}

func (discardWriteCloser) Write(p []byte) (int, error) { return len(p), nil }
func (discardWriteCloser) Close() error                { return nil }

// TestStdioCallReturnsOnContextCancel pins that a stdio call unblocks when its
// context is cancelled even though the server never replies. The stdio child is
// bound to the session, not the turn, so without this a hung server would hang a
// cancelled turn forever. No reader goroutine runs here, so the reply never
// arrives — only ctx cancellation can return the call.
func TestStdioCallReturnsOnContextCancel(t *testing.T) {
	tr := &stdioTransport{
		name:    "hung",
		stdin:   discardWriteCloser{},
		pending: map[int]chan rpcResponse{},
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := tr.call(ctx, "tools/call", map[string]any{})
		done <- err
	}()

	time.Sleep(100 * time.Millisecond) // let the call park in its select
	cancel()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancelled call returned nil error")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("stdio call did not return within 2s of ctx cancel — a hung server hangs the turn")
	}
}

func TestStdioCallRespectsExistingDeadline(t *testing.T) {
	tr := &stdioTransport{
		name:    "server",
		stdin:   discardWriteCloser{},
		pending: map[int]chan rpcResponse{},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := tr.call(ctx, "tools/call", map[string]any{})
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("timed-out call returned nil error")
		}
	case <-time.After(1 * time.Second):
		t.Fatal("stdio call did not return within caller deadline")
	}
}

func TestStdioCallCancelReturnsContextCanceled(t *testing.T) {
	tr := &stdioTransport{
		name:    "slow-server",
		stdin:   discardWriteCloser{},
		pending: map[int]chan rpcResponse{},
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := tr.call(ctx, "tools/call", map[string]any{})
		done <- err
	}()

	time.Sleep(200 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("cancelled call returned nil error")
		}
		if err != context.Canceled {
			t.Fatalf("expected context.Canceled, got: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("stdio call did not return within 2s of cancel")
	}
}

// Some MCP servers send capability-change notifications and a ping while the
// initialize call is in flight. The server must receive its ping response
// before it can finish the handshake; dropping server requests deadlocks both
// sides even though notifications themselves are harmless.
func TestStdioInitializeHandlesNotificationsAndServerPing(t *testing.T) {
	workspaceRoot := t.TempDir()
	serverReads, clientWrites := io.Pipe()
	clientReads, serverWrites := io.Pipe()
	t.Cleanup(func() {
		_ = clientWrites.Close()
		_ = serverReads.Close()
		_ = serverWrites.Close()
		_ = clientReads.Close()
	})

	tr := &stdioTransport{
		name:    "matlab",
		roots:   mcpRoots(workspaceRoot),
		stdin:   clientWrites,
		stdout:  bufio.NewReader(clientReads),
		stderr:  &tailBuffer{limit: 1024},
		pending: map[int]chan rpcResponse{},
	}
	go tr.readLoop()

	serverDone := make(chan error, 1)
	go func() {
		dec := json.NewDecoder(serverReads)
		enc := json.NewEncoder(serverWrites)
		var initialize struct {
			ID     int    `json:"id"`
			Method string `json:"method"`
			Params struct {
				Capabilities map[string]json.RawMessage `json:"capabilities"`
			} `json:"params"`
		}
		if err := dec.Decode(&initialize); err != nil {
			serverDone <- fmt.Errorf("decode initialize: %w", err)
			return
		}
		if initialize.Method != "initialize" {
			serverDone <- fmt.Errorf("first method = %q, want initialize", initialize.Method)
			return
		}
		if _, ok := initialize.Params.Capabilities["roots"]; !ok {
			serverDone <- fmt.Errorf("initialize capabilities = %v, want roots", initialize.Params.Capabilities)
			return
		}
		for _, method := range []string{"notifications/tools/list_changed", "notifications/resources/list_changed"} {
			if err := enc.Encode(map[string]any{"jsonrpc": "2.0", "method": method}); err != nil {
				serverDone <- fmt.Errorf("encode %s: %w", method, err)
				return
			}
		}
		if err := enc.Encode(map[string]any{"jsonrpc": "2.0", "id": "server-roots", "method": "roots/list"}); err != nil {
			serverDone <- fmt.Errorf("encode roots/list: %w", err)
			return
		}
		var rootsResponse struct {
			ID     string `json:"id"`
			Result struct {
				Roots []mcpRoot `json:"roots"`
			} `json:"result"`
		}
		if err := dec.Decode(&rootsResponse); err != nil {
			serverDone <- fmt.Errorf("decode roots/list response: %w", err)
			return
		}
		wantRoots := mcpRoots(workspaceRoot)
		if rootsResponse.ID != "server-roots" || len(rootsResponse.Result.Roots) != 1 || rootsResponse.Result.Roots[0] != wantRoots[0] {
			serverDone <- fmt.Errorf("roots/list response = %+v, want %+v", rootsResponse, wantRoots)
			return
		}
		if err := enc.Encode(map[string]any{"jsonrpc": "2.0", "id": "server-ping", "method": "ping"}); err != nil {
			serverDone <- fmt.Errorf("encode ping: %w", err)
			return
		}
		var pingResponse struct {
			ID     string         `json:"id"`
			Result map[string]any `json:"result"`
		}
		if err := dec.Decode(&pingResponse); err != nil {
			serverDone <- fmt.Errorf("decode ping response: %w", err)
			return
		}
		if pingResponse.ID != "server-ping" || pingResponse.Result == nil {
			serverDone <- fmt.Errorf("ping response = %+v", pingResponse)
			return
		}
		if err := enc.Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      initialize.ID,
			"result": map[string]any{
				"protocolVersion": protocolVersion,
				"serverInfo":      map[string]any{"name": "matlab", "version": "0.11.2"},
				"capabilities":    map[string]any{},
			},
		}); err != nil {
			serverDone <- fmt.Errorf("encode initialize response: %w", err)
			return
		}
		var initialized struct {
			Method string `json:"method"`
		}
		if err := dec.Decode(&initialized); err != nil {
			serverDone <- fmt.Errorf("decode initialized notification: %w", err)
			return
		}
		if initialized.Method != "notifications/initialized" {
			serverDone <- fmt.Errorf("final method = %q, want notifications/initialized", initialized.Method)
			return
		}
		serverDone <- nil
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	client := &Client{name: "matlab", t: tr, spec: Spec{WorkspaceRoot: workspaceRoot}}
	if err := client.initialize(ctx); err != nil {
		t.Fatalf("initialize with server notifications and ping: %v", err)
	}
	select {
	case err := <-serverDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("server did not complete the MCP initialization handshake")
	}
}

func TestStdioToolCallRoutesProgressNotification(t *testing.T) {
	serverReads, clientWrites := io.Pipe()
	clientReads, serverWrites := io.Pipe()
	t.Cleanup(func() {
		_ = clientWrites.Close()
		_ = serverReads.Close()
		_ = serverWrites.Close()
		_ = clientReads.Close()
	})

	tr := &stdioTransport{
		name:    "worker",
		stdin:   clientWrites,
		stdout:  bufio.NewReader(clientReads),
		stderr:  &tailBuffer{limit: 1024},
		pending: map[int]chan rpcResponse{},
	}
	go tr.readLoop()

	serverDone := make(chan error, 1)
	go func() {
		dec := json.NewDecoder(serverReads)
		enc := json.NewEncoder(serverWrites)
		var request struct {
			ID     int    `json:"id"`
			Method string `json:"method"`
			Params struct {
				Meta map[string]any `json:"_meta"`
			} `json:"params"`
		}
		if err := dec.Decode(&request); err != nil {
			serverDone <- err
			return
		}
		token, _ := request.Params.Meta["progressToken"].(string)
		if request.Method != "tools/call" || token == "" {
			serverDone <- fmt.Errorf("tools/call request = %+v, want progressToken", request)
			return
		}
		if err := enc.Encode(map[string]any{
			"jsonrpc": "2.0",
			"method":  "notifications/progress",
			"params": map[string]any{
				"progressToken": token,
				"progress":      2,
				"total":         5,
				"message":       "Indexing",
			},
		}); err != nil {
			serverDone <- err
			return
		}
		if err := enc.Encode(map[string]any{"jsonrpc": "2.0", "id": request.ID, "result": map[string]any{"content": []any{}}}); err != nil {
			serverDone <- err
			return
		}
		serverDone <- nil
	}()

	progress := make(chan string, 1)
	ctx := tool.WithProgress(context.Background(), func(chunk string) { progress <- chunk })
	client := &Client{name: "worker", t: tr}
	if _, err := client.call(ctx, "tools/call", map[string]any{"name": "index", "arguments": map[string]any{}}); err != nil {
		t.Fatalf("tools/call: %v", err)
	}
	select {
	case got := <-progress:
		if got != "Indexing (2/5)\n" {
			t.Fatalf("progress = %q", got)
		}
	case <-time.After(time.Second):
		t.Fatal("progress notification was not routed")
	}
	if err := <-serverDone; err != nil {
		t.Fatal(err)
	}
}

// readLoop is the only goroutine draining stdout, so it must never block on
// the shared stdin pipe: with both pipe buffers full, waiting on writeMu would
// deadlock against a client call whose own stdin write is jammed. Replies to
// server requests therefore go through a bounded queue that drops on overflow.
func TestStdioReadLoopStaysLiveWhenReplyWriterIsBlocked(t *testing.T) {
	stdinReads, stdinWrites := io.Pipe() // nobody reads: reply writes block forever
	stdoutReads, stdoutWrites := io.Pipe()
	t.Cleanup(func() {
		_ = stdinReads.Close()
		_ = stdinWrites.Close()
		_ = stdoutReads.Close()
		_ = stdoutWrites.Close()
	})

	tr := &stdioTransport{
		name:    "jammed",
		stdin:   stdinWrites,
		stdout:  bufio.NewReader(stdoutReads),
		stderr:  &tailBuffer{limit: 1024},
		pending: map[int]chan rpcResponse{},
	}
	waiting := make(chan rpcResponse, 1)
	tr.pending[7] = waiting
	go tr.readLoop()

	// Flood well past the reply queue bound while the reply writer is stuck in
	// its first stdin write; overflow must drop, not block readLoop. The writes
	// run off the test goroutine so a deadlocked readLoop fails the timeout
	// below instead of hanging the whole package; Cleanup unblocks the writer.
	go func() {
		for i := 0; i < 2*stdioReplyQueueBound; i++ {
			line := fmt.Sprintf(`{"jsonrpc":"2.0","id":"srv-%d","method":"ping"}`+"\n", i)
			if _, err := io.WriteString(stdoutWrites, line); err != nil {
				return
			}
		}
		_, _ = io.WriteString(stdoutWrites, `{"jsonrpc":"2.0","id":7,"result":{}}`+"\n")
	}()

	select {
	case resp := <-waiting:
		if resp.ID != 7 {
			t.Fatalf("routed response id = %d, want 7", resp.ID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("readLoop stopped routing responses while the reply writer was blocked")
	}
}
