// Package attach implements `reasonix remote attach-workspace --stdio`.
// It validates initialize (Schema Hash), starts or reuses the workspace
// runtime, and becomes a transparent Unix-socket proxy for bidirectional RPC.
package attach

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"reasonix/internal/proc"
	"reasonix/internal/remote/protocol"
	"reasonix/internal/remote/workbench/runtime"
	"reasonix/internal/rpcwire"
)

// Options configures attach-workspace.
type Options struct {
	// Workspace optionally binds attach to a CLI/env target. When empty, the
	// authenticated initialize DTO selects the workspace.
	Workspace string
	// Home is the remote user home for socket placement.
	Home string
	// Version is the product version string for diagnostics.
	Version string
	// SchemaHash expected; empty uses protocol.SchemaHash().
	SchemaHash string
	// RuntimeBinary is optional path to reasonix for launching runtime child.
	// When empty, attach serves the runtime in-process (tests / single binary).
	RuntimeBinary string
	// InProcess when true always serves runtime in this process (tests).
	InProcess bool
}

// Run reads the first initialize frame, ensures runtime for workspace, then
// proxies stdio ↔ Unix socket until EOF.
func Run(ctx context.Context, stdin io.ReadCloser, stdout io.Writer, opts Options) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if stdin == nil || stdout == nil {
		return errors.New("attach-workspace requires stdio")
	}
	home := strings.TrimSpace(opts.Home)
	if home == "" {
		var err error
		home, err = os.UserHomeDir()
		if err != nil {
			return err
		}
	}
	configuredWorkspace := ""
	if strings.TrimSpace(opts.Workspace) != "" {
		var err error
		configuredWorkspace, err = resolveWorkspacePath(opts.Workspace, home)
		if err != nil {
			return fmt.Errorf("resolve workspace: %w", err)
		}
	}
	schemaHash := strings.TrimSpace(opts.SchemaHash)
	if schemaHash == "" {
		schemaHash = protocol.SchemaHash()
	}

	reader := bufio.NewReaderSize(stdin, 64<<10)
	stop := context.AfterFunc(ctx, func() { _ = stdin.Close() })
	frame, err := rpcwire.ReadStrictRequestFrame(reader, protocol.FrameBytes)
	stop()
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		return fmt.Errorf("attach bootstrap: %w", err)
	}
	if protocol.Method(frame.Method) != protocol.MethodRemoteInitialize {
		return writeRPCError(stdout, frame.ID, rpcwire.ErrInvalidRequest, "remote/initialize must be first")
	}
	decoded, err := protocol.DecodeRequestParams(protocol.MethodRemoteInitialize, frame.Params)
	if err != nil {
		return writeRPCError(stdout, frame.ID, rpcwire.ErrInvalidParams, "invalid remote/initialize params")
	}
	init := decoded.(protocol.InitializeParams)
	ws, err := resolveWorkspacePath(init.Workspace, home)
	if err != nil {
		return writeRPCError(stdout, frame.ID, rpcwire.ErrInvalidParams, "invalid Remote workspace")
	}
	if configuredWorkspace != "" && filepath.Clean(ws) != filepath.Clean(configuredWorkspace) {
		return writeRPCError(stdout, frame.ID, rpcwire.ErrInvalidParams, "Remote workspace does not match attach target")
	}
	// The runtime is started with the canonical absolute workspace. Forward the
	// same value so `~` and relative aliases cannot diverge at its second gate.
	init.Workspace = ws
	peerHash := strings.TrimSpace(init.BuildID.SchemaHash)
	if !strings.EqualFold(peerHash, schemaHash) {
		return writeRPCError(stdout, frame.ID, rpcwire.ErrInvalidRequest,
			fmt.Sprintf("schema hash mismatch: expected %s", schemaHash))
	}

	sock := runtime.SocketPath(home, ws)
	if err := ensureRuntime(ctx, opts, home, ws, sock); err != nil {
		return writeRPCError(stdout, frame.ID, rpcwire.ErrInternal, "runtime start failed: "+err.Error())
	}

	conn, err := dialSocketUntil(ctx, sock, 10*time.Second)
	if err != nil {
		return writeRPCError(stdout, frame.ID, rpcwire.ErrInternal, "runtime dial failed")
	}
	defer conn.Close()

	// Re-encode only the frozen typed fields. This prevents bootstrap-only or
	// legacy fields from bypassing the runtime Router's strict DTO boundary.
	forward, err := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": json.RawMessage(frame.ID),
		"method": string(protocol.MethodRemoteInitialize), "params": init,
	})
	if err != nil {
		return fmt.Errorf("encode initialize: %w", err)
	}
	if _, err := conn.Write(append(forward, '\n')); err != nil {
		return fmt.Errorf("forward initialize: %w", err)
	}
	return proxy(ctx, stdin, reader, stdout, conn)
}

func resolveWorkspacePath(raw, home string) (string, error) {
	workspace := strings.TrimSpace(raw)
	if workspace == "" {
		return "", errors.New("workspace is required")
	}
	switch {
	case workspace == "~":
		workspace = home
	case strings.HasPrefix(workspace, "~/"):
		workspace = filepath.Join(home, strings.TrimPrefix(workspace, "~/"))
	}
	abs, err := filepath.Abs(workspace)
	if err != nil {
		return "", err
	}
	return filepath.Clean(abs), nil
}

func ensureRuntime(ctx context.Context, opts Options, home, workspace, sock string) error {
	// Try dial first (reuse live runtime).
	if c, err := dialSocket(ctx, sock, 200*time.Millisecond); err == nil {
		_ = c.Close()
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(sock), 0o700); err != nil {
		return fmt.Errorf("create runtime socket directory: %w", err)
	}
	lockDir := sock + ".start.lock"
	deadline := time.Now().Add(10 * time.Second)
	for {
		if err := os.Mkdir(lockDir, 0o700); err == nil {
			defer os.Remove(lockDir)
			break
		} else if !os.IsExist(err) {
			return err
		}
		if c, err := dialSocket(ctx, sock, 200*time.Millisecond); err == nil {
			_ = c.Close()
			return nil
		}
		if time.Now().After(deadline) {
			return errors.New("timed out waiting for workspace runtime startup lease")
		}
		time.Sleep(100 * time.Millisecond)
	}
	// Re-check after taking the lease: another attach may have started it.
	if c, err := dialSocket(ctx, sock, 200*time.Millisecond); err == nil {
		_ = c.Close()
		return nil
	}
	if opts.InProcess {
		// In-process: start listener in background of this process.
		// For production CLI, prefer a detached child; tests use InProcess.
		srv := runtime.New(runtime.Options{Workspace: workspace, Version: opts.Version})
		go func() { _ = srv.ListenAndServe(ctx, sock) }()
		// The caller's real attach connection performs bounded retries. Do not use
		// a readiness dial here: runtime accepts are generation-owning connections,
		// and Windows AF_UNIX does not expose a portable socket file to os.Stat.
		return nil
	}
	if strings.TrimSpace(opts.RuntimeBinary) == "" {
		return errors.New("runtime binary required outside tests")
	}
	// Detached child: reasonix remote-runtime-workbench --workspace --socket
	cmd := exec.Command(opts.RuntimeBinary, "remote", "runtime-workbench",
		"--workspace", workspace, "--socket", sock, "--version", opts.Version)
	cmd.Stdout = nil
	cmd.Stderr = nil
	if _, err := startRuntimeProcess(cmd); err != nil {
		return err
	}
	deadline = time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if c, err := dialSocket(ctx, sock, 200*time.Millisecond); err == nil {
			_ = c.Close()
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return errors.New("runtime child did not become ready")
}

func startRuntimeProcess(cmd *exec.Cmd) (<-chan error, error) {
	// The runtime must outlive the SSH command process during the detach grace
	// period. A new session prevents sshd from forwarding the transport's
	// SIGHUP to the runtime, while Wait keeps short-lived children out of the
	// zombie state until the attach process exits.
	proc.SetProcessGroupKill(cmd)
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
		close(done)
	}()
	return done, nil
}

func dialSocket(ctx context.Context, sock string, timeout time.Duration) (net.Conn, error) {
	d := net.Dialer{Timeout: timeout}
	return d.DialContext(ctx, "unix", sock)
}

func dialSocketUntil(ctx context.Context, sock string, timeout time.Duration) (net.Conn, error) {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			if lastErr == nil {
				lastErr = context.DeadlineExceeded
			}
			return nil, lastErr
		}
		attempt := 200 * time.Millisecond
		if remaining < attempt {
			attempt = remaining
		}
		conn, err := dialSocket(ctx, sock, attempt)
		if err == nil {
			return conn, nil
		}
		lastErr = err
		timer := time.NewTimer(25 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

func proxy(ctx context.Context, stdin io.ReadCloser, reader *bufio.Reader, stdout io.Writer, conn net.Conn) error {
	errCh := make(chan error, 2)
	go func() {
		// Drain buffered remainder then stdin → conn.
		if reader.Buffered() > 0 {
			buf := make([]byte, reader.Buffered())
			_, _ = reader.Read(buf)
			if _, err := conn.Write(buf); err != nil {
				errCh <- err
				_ = conn.Close()
				return
			}
		}
		_, err := io.Copy(conn, stdin)
		errCh <- err
		_ = conn.Close()
	}()
	go func() {
		_, err := io.Copy(stdout, conn)
		errCh <- err
		_ = stdin.Close()
	}()
	select {
	case <-ctx.Done():
		_ = conn.Close()
		_ = stdin.Close()
		return ctx.Err()
	case err := <-errCh:
		_ = conn.Close()
		_ = stdin.Close()
		if err == nil || errors.Is(err, io.EOF) {
			return nil
		}
		return err
	}
}

func writeRPCError(w io.Writer, id json.RawMessage, code int, message string) error {
	if !json.Valid(bytes.TrimSpace(id)) {
		id = json.RawMessage("null")
	}
	frame := map[string]any{
		"jsonrpc": "2.0",
		"id":      json.RawMessage(id),
		"error":   map[string]any{"code": code, "message": message},
	}
	data, err := json.Marshal(frame)
	if err != nil {
		return err
	}
	_, err = w.Write(append(data, '\n'))
	return err
}
