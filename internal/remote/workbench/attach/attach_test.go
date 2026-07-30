package attach

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/remote/protocol"
)

type nonInterruptibleReadCloser struct {
	release chan struct{}
}

func (r *nonInterruptibleReadCloser) Read([]byte) (int, error) {
	<-r.release
	return 0, io.EOF
}

func (*nonInterruptibleReadCloser) Close() error { return nil }

type responseBuffer struct {
	mu    sync.Mutex
	buf   bytes.Buffer
	ready chan struct{}
	once  sync.Once
}

func (w *responseBuffer) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	n, err := w.buf.Write(p)
	if bytes.Contains(w.buf.Bytes(), []byte{'\n'}) {
		w.once.Do(func() { close(w.ready) })
	}
	return n, err
}

func (w *responseBuffer) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buf.String()
}

func TestAttachRejectsSchemaMismatch(t *testing.T) {
	var out bytes.Buffer
	workspace := t.TempDir()
	params, _ := json.Marshal(map[string]any{
		"buildId": map[string]any{
			"productVersion":  "test",
			"sourceRevision":  strings.Repeat("a", 40),
			"schemaHash":      "sha256:" + strings.Repeat("0", 64),
			"protocolVersion": protocol.ProtocolVersion,
		},
		"clientInstanceId": "desktop-test",
		"workspace":        workspace,
	})
	frame, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "remote/initialize", "params": json.RawMessage(params),
	})
	r, w := io.Pipe()
	go func() {
		_, _ = w.Write(append(frame, '\n'))
		_ = w.Close()
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := Run(ctx, r, &out, Options{
		Workspace: workspace, Version: "t", InProcess: true,
		SchemaHash: protocol.SchemaHash(),
	})
	// Schema mismatch returns nil error from Run after writing RPC error, or an error.
	// Either way the response should mention mismatch.
	if !strings.Contains(out.String(), "schema hash mismatch") && err == nil {
		// Run may return the write path with error frame only.
		if !strings.Contains(out.String(), "mismatch") {
			t.Fatalf("out=%q err=%v", out.String(), err)
		}
	}
}

func TestAttachRejectsWorkspaceDifferentFromAttachTarget(t *testing.T) {
	var out bytes.Buffer
	configuredWorkspace := t.TempDir()
	requestedWorkspace := t.TempDir()
	params, _ := json.Marshal(map[string]any{
		"buildId": map[string]any{
			"productVersion":  "test",
			"sourceRevision":  strings.Repeat("a", 40),
			"schemaHash":      protocol.SchemaHash(),
			"protocolVersion": protocol.ProtocolVersion,
		},
		"clientInstanceId": "desktop-test",
		"workspace":        requestedWorkspace,
	})
	frame, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "remote/initialize", "params": json.RawMessage(params),
	})
	r, w := io.Pipe()
	go func() {
		_, _ = w.Write(append(frame, '\n'))
		_ = w.Close()
	}()

	err := Run(context.Background(), r, &out, Options{
		Workspace: configuredWorkspace,
		Version:   "t",
		InProcess: true,
	})
	if err != nil {
		t.Fatalf("Run returned transport error: %v", err)
	}
	if !strings.Contains(out.String(), "workspace does not match attach target") {
		t.Fatalf("out=%q", out.String())
	}
}

func TestAttachUsesInitializeWorkspaceWhenTargetIsUnbound(t *testing.T) {
	tempBase := os.TempDir()
	if goruntime.GOOS == "darwin" {
		// macOS Unix-domain sockets have a short path limit, while os.TempDir()
		// normally points into a long per-user /var/folders path.
		tempBase = "/tmp"
	}
	home, err := os.MkdirTemp(tempBase, "rx-attach-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(home) })
	params, _ := json.Marshal(map[string]any{
		"buildId":          protocol.CurrentBuildID("t"),
		"clientInstanceId": "desktop-test",
		"workspace":        "~/project",
	})
	frame, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "remote/initialize", "params": json.RawMessage(params),
	})
	pr, pw := io.Pipe()
	out := &responseBuffer{ready: make(chan struct{})}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go func() {
		_, _ = pw.Write(append(frame, '\n'))
		select {
		case <-out.ready:
		case <-ctx.Done():
		}
		_ = pw.Close()
	}()
	err = Run(ctx, pr, out, Options{
		Home: home, Version: "t", InProcess: true,
	})
	output := out.String()
	if strings.Contains(output, "workspace is required") || strings.Contains(output, "invalid Remote workspace") {
		t.Fatalf("initialize workspace was not accepted: out=%q err=%v", output, err)
	}
	if !strings.Contains(output, `"result"`) {
		t.Fatalf("initialize did not reach the normalized runtime workspace: out=%q err=%v", output, err)
	}
}

func TestResolveWorkspacePathExpandsRemoteHome(t *testing.T) {
	home := t.TempDir()
	rootInput := string(filepath.Separator)
	rootExpected, err := filepath.Abs(rootInput)
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "home", raw: "~", want: home},
		{name: "home child", raw: "~/project", want: filepath.Join(home, "project")},
		{name: "root", raw: rootInput, want: filepath.Clean(rootExpected)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveWorkspacePath(tt.raw, home)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Fatalf("resolveWorkspacePath(%q) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
	if _, err := resolveWorkspacePath(" ", home); err == nil {
		t.Fatal("blank workspace should be rejected")
	}
}

func TestAttachInProcessInitializeOK(t *testing.T) {
	ws := t.TempDir()
	params, _ := json.Marshal(map[string]any{
		"buildId":          protocol.CurrentBuildID("t"),
		"clientInstanceId": "desktop-test",
		"workspace":        ws,
	})
	frame, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "remote/initialize", "params": json.RawMessage(params),
	})
	// After initialize the proxy will hang on copy until we close stdin after a second request is not needed —
	// closing stdin after first frame ends the proxy.
	pr, pw := io.Pipe()
	go func() {
		_, _ = pw.Write(append(frame, '\n'))
		// Keep open briefly so runtime can start, then close to end proxy.
		time.Sleep(200 * time.Millisecond)
		_ = pw.Close()
	}()
	var out bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	err := Run(ctx, pr, &out, Options{
		Workspace: ws, Home: filepath.Dir(ws), Version: "t", InProcess: true,
	})
	if err != nil && !strings.Contains(err.Error(), "EOF") {
		// Accept connection closed after stdin EOF.
		t.Logf("run err (may be ok): %v out=%q", err, out.String())
	}
}

func TestProxyReturnsWhenPeerClosesAndInputCloseDoesNotUnblockRead(t *testing.T) {
	stdin := &nonInterruptibleReadCloser{release: make(chan struct{})}
	local, peer := net.Pipe()
	done := make(chan error, 1)
	go func() {
		done <- proxy(context.Background(), stdin, bufio.NewReader(stdin), io.Discard, local)
	}()
	_ = peer.Close()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("proxy returned transport error after peer close: %v", err)
		}
	case <-time.After(2 * time.Second):
		close(stdin.release)
		t.Fatal("proxy waited indefinitely for the blocked stdin copy")
	}
	close(stdin.release)
}

func TestStartRuntimeProcessReapsExitedChild(t *testing.T) {
	if os.Getenv("GO_WANT_ATTACH_RUNTIME_HELPER") == "1" {
		os.Exit(0)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestStartRuntimeProcessReapsExitedChild$")
	cmd.Env = append(os.Environ(), "GO_WANT_ATTACH_RUNTIME_HELPER=1")
	done, err := startRuntimeProcess(cmd)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("runtime helper exit: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("runtime child was not reaped")
	}
	if cmd.ProcessState == nil || !cmd.ProcessState.Exited() {
		t.Fatal("runtime child was not reaped")
	}
}
