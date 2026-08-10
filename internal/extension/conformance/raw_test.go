package conformance

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/extension/protocol"
	"reasonix/internal/extension/rpcwire"
)

// rawSidecar drives the example binary directly over hand-written JSON-RPC
// frames, for the transport-level conformance cases the typed host client
// cannot produce (unregistered methods, oversized frames, exit statuses).
type rawSidecar struct {
	t      *testing.T
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout *bufio.Reader
	stderr *bytes.Buffer

	nextID   int64
	waitOnce sync.Once
	waitErr  error
}

type rawFrame struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code    int             `json:"code"`
		Message string          `json:"message"`
		Data    json.RawMessage `json:"data"`
	} `json:"error"`
}

func startRawSidecar(t *testing.T) *rawSidecar {
	t.Helper()
	cmd := exec.Command(examplePath)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("StdinPipe: %v", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatalf("StdoutPipe: %v", err)
	}
	stderr := &bytes.Buffer{}
	cmd.Stderr = stderr
	if err := cmd.Start(); err != nil {
		t.Fatalf("start example: %v", err)
	}
	r := &rawSidecar{t: t, cmd: cmd, stdin: stdin, stdout: bufio.NewReader(stdout), stderr: stderr}
	t.Cleanup(func() {
		if r.cmd.Process != nil {
			_ = r.cmd.Process.Kill()
		}
		_ = r.wait()
	})
	return r
}

// wait reaps the process exactly once.
func (r *rawSidecar) wait() error {
	r.waitOnce.Do(func() { r.waitErr = r.cmd.Wait() })
	return r.waitErr
}

// waitWithin reaps the process inside the budget.
func (r *rawSidecar) waitWithin(what string, budget time.Duration) error {
	done := make(chan error, 1)
	go func() { done <- r.wait() }()
	select {
	case err := <-done:
		return err
	case <-time.After(budget):
		r.t.Fatalf("process did not exit within %s (%s)", budget, what)
		return nil
	}
}

// send marshals and writes one frame.
func (r *rawSidecar) send(v any) {
	r.t.Helper()
	raw, err := json.Marshal(v)
	if err != nil {
		r.t.Fatalf("marshal frame: %v", err)
	}
	if _, err := r.stdin.Write(append(raw, '\n')); err != nil {
		r.t.Fatalf("write frame: %v", err)
	}
}

// readFrame reads one NDJSON frame.
func (r *rawSidecar) readFrame() (rawFrame, error) {
	line, err := r.stdout.ReadBytes('\n')
	if err != nil {
		return rawFrame{}, err
	}
	var frame rawFrame
	if err := json.Unmarshal(line, &frame); err != nil {
		return rawFrame{}, err
	}
	return frame, nil
}

// call writes one request and returns its response, failing on any
// interleaved host-bound traffic (none is expected in these scenarios).
func (r *rawSidecar) call(method string, params any) rawFrame {
	r.t.Helper()
	r.nextID++
	id := r.nextID
	r.send(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	for {
		frame, err := r.readFrame()
		if err != nil {
			r.t.Fatalf("read answer for %s: %v (stderr: %s)", method, err, strings.TrimSpace(r.stderr.String()))
		}
		if frame.Method != "" {
			r.t.Fatalf("extension sent an unexpected host-bound request %q", frame.Method)
		}
		var gotID int64
		if err := json.Unmarshal(frame.ID, &gotID); err == nil && gotID == id {
			return frame
		}
	}
}

// handshake runs the initialize exchange and opens the barrier.
func (r *rawSidecar) handshake() {
	r.t.Helper()
	frame := r.call("extension/initialize", protocol.InitializeParams{
		ProtocolVersion: protocol.ProtocolVersion,
		ProtocolID:      protocol.ProtocolID,
		Session:         protocol.SessionContext{SessionID: "raw-sess", WorkspaceRoot: "/ws", Generation: 1},
		Capabilities:    protocol.HostCapabilities{ContentRefs: true, UIHost: protocol.UIHostHeadless, ProtocolVersion: protocol.ProtocolVersion},
	})
	if frame.Error != nil {
		r.t.Fatalf("initialize answered with an error: %+v", frame.Error)
	}
	var result protocol.InitializeResult
	if err := json.Unmarshal(frame.Result, &result); err != nil {
		r.t.Fatalf("decode initialize result: %v", err)
	}
	r.send(map[string]any{"jsonrpc": "2.0", "method": "extension/initialized", "params": map[string]any{}})
}

// TestUnknownMethod sends a request for an unregistered method past the
// handshake: the SDK must answer with the JSON-RPC method-not-found code and
// the frozen unknown_method reason.
func TestUnknownMethod(t *testing.T) {
	r := startRawSidecar(t)
	r.handshake()

	frame := r.call("extension/bogus", map[string]any{})
	if frame.Error == nil {
		t.Fatalf("unknown method answered with result %s", string(frame.Result))
	}
	if frame.Error.Code != rpcwire.ErrMethodNotFound {
		t.Fatalf("error code = %d, want %d", frame.Error.Code, rpcwire.ErrMethodNotFound)
	}
	var data protocol.ProtocolErrorData
	if err := json.Unmarshal(frame.Error.Data, &data); err != nil {
		t.Fatalf("error data does not decode: %v", err)
	}
	if data.Reason != protocol.ErrUnknownMethod {
		t.Fatalf("error reason = %q, want %q", data.Reason, protocol.ErrUnknownMethod)
	}
}

// TestOversizedFrame sends one NDJSON line beyond the frozen 8 MiB frame
// budget: the SDK must fail the connection and exit non-zero.
func TestOversizedFrame(t *testing.T) {
	r := startRawSidecar(t)
	line := strings.Repeat("a", protocol.FrameBytes+1024)
	go func() {
		// The write may fail with EPIPE once the SDK drops the connection;
		// either way the connection error is what is being asserted.
		_, _ = io.WriteString(r.stdin, line+"\n")
	}()
	if err := r.waitWithin("oversized frame", 15*time.Second); err == nil {
		t.Fatal("process exited 0 after an oversized frame")
	}
	if !strings.Contains(r.stderr.String(), "frame") {
		t.Fatalf("stderr does not mention the frame violation: %q", strings.TrimSpace(r.stderr.String()))
	}
}

// TestBoundedShutdownExitZero runs the orderly shutdown: the example answers
// accepted:true and the process exits 0 inside the budget.
func TestBoundedShutdownExitZero(t *testing.T) {
	r := startRawSidecar(t)
	r.handshake()

	frame := r.call("extension/shutdown", protocol.ShutdownParams{TimeoutMillis: 5000})
	if frame.Error != nil {
		t.Fatalf("shutdown answered with an error: %+v", frame.Error)
	}
	var result protocol.ShutdownResult
	if err := json.Unmarshal(frame.Result, &result); err != nil {
		t.Fatalf("decode shutdown result: %v", err)
	}
	if !result.Accepted {
		t.Fatalf("shutdown not accepted: %+v", result)
	}
	// The real host closes the sidecar's stdin right after the shutdown
	// request (proc.close); the SDK then sees EOF, Serve returns nil, and
	// the process exits 0.
	if err := r.stdin.Close(); err != nil {
		t.Fatalf("close stdin: %v", err)
	}
	if err := r.waitWithin("shutdown", 10*time.Second); err != nil {
		t.Fatalf("process exit = %v, want 0 (stderr: %s)", err, strings.TrimSpace(r.stderr.String()))
	}
}
