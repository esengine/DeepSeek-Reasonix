package acp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// requester is the slice of Conn that clientIO drives: agent → client requests.
type requester interface {
	Request(ctx context.Context, method string, params any) (json.RawMessage, error)
}

// clientIO implements builtin.FileOverlay and builtin.TerminalRunner over the
// ACP connection for one session, backed by the capabilities the client
// declared at initialize. Every method degrades to "not handled" (ok=false) on
// a missing capability or a transport/client error, so the tools fall back to
// their local implementations instead of failing the call.
type clientIO struct {
	conn      requester
	sessionID string
	caps      ClientCapabilities
}

func newClientIO(conn requester, sessionID string, caps ClientCapabilities) *clientIO {
	return &clientIO{conn: conn, sessionID: sessionID, caps: caps}
}

// hasAny reports whether the client offered anything clientIO can use; callers
// skip wiring the overlay/terminal entirely when it is false.
func (c *clientIO) hasAny() bool {
	return c.caps.FS.ReadTextFile || c.caps.FS.WriteTextFile || c.caps.Terminal
}

// fileOverlay returns c when the client offers an fs method, else nil. The nil
// is typed at the call site (assigned to the interface field only when
// non-nil), so a session without fs capability carries a nil interface.
func (c *clientIO) fileOverlay() *clientIO {
	if c.caps.FS.ReadTextFile || c.caps.FS.WriteTextFile {
		return c
	}
	return nil
}

// terminalRunner returns c when the client offers terminals, else nil.
func (c *clientIO) terminalRunner() *clientIO {
	if c.caps.Terminal {
		return c
	}
	return nil
}

// ReadTextFile implements builtin.FileOverlay: the client's view of the file,
// including unsaved editor buffers. ok=false on missing capability or any
// client/transport error — the tool then reads the local disk.
func (c *clientIO) ReadTextFile(ctx context.Context, path string) (string, bool) {
	if !c.caps.FS.ReadTextFile {
		return "", false
	}
	raw, err := c.conn.Request(ctx, "fs/read_text_file", FSReadTextFileParams{SessionID: c.sessionID, Path: path})
	if err != nil {
		return "", false
	}
	var res FSReadTextFileResult
	if json.Unmarshal(raw, &res) != nil {
		return "", false
	}
	return res.Content, true
}

// WriteTextFile implements builtin.FileOverlay: the client writes content to
// path and updates any open buffer. ok=false on missing capability so the tool
// writes the local disk; with the capability present, a client error is a real
// write failure and is surfaced (falling back could double-apply).
func (c *clientIO) WriteTextFile(ctx context.Context, path, content string) (bool, error) {
	if !c.caps.FS.WriteTextFile {
		return false, nil
	}
	if _, err := c.conn.Request(ctx, "fs/write_text_file", FSWriteTextFileParams{SessionID: c.sessionID, Path: path, Content: content}); err != nil {
		return true, err
	}
	return true, nil
}

// terminalOutputByteLimit bounds how much output a client terminal buffers for
// one command; matches the local bash tool's practical output scale.
const terminalOutputByteLimit = 1 << 20

// RunCommand implements builtin.TerminalRunner: run the command in a
// client-owned terminal (terminal/create → wait_for_exit → output → release)
// so the user watches it live. ok=false when the client has no terminal
// capability or creation fails — the bash tool then executes locally. A
// timeout kills the terminal and returns what it printed.
func (c *clientIO) RunCommand(ctx context.Context, command, cwd string, timeout time.Duration) (string, bool, error) {
	if !c.caps.Terminal {
		return "", false, nil
	}
	raw, err := c.conn.Request(ctx, "terminal/create", TerminalCreateParams{
		SessionID:       c.sessionID,
		Command:         command,
		Cwd:             cwd,
		OutputByteLimit: terminalOutputByteLimit,
	})
	if err != nil {
		return "", false, nil
	}
	var created TerminalCreateResult
	if json.Unmarshal(raw, &created) != nil || strings.TrimSpace(created.TerminalID) == "" {
		return "", false, nil
	}
	id := TerminalIDParams{SessionID: c.sessionID, TerminalID: created.TerminalID}
	defer func() { _, _ = c.conn.Request(context.WithoutCancel(ctx), "terminal/release", id) }()

	waitCtx := ctx
	var cancel context.CancelFunc
	if timeout > 0 {
		waitCtx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	_, waitErr := c.conn.Request(waitCtx, "terminal/wait_for_exit", id)
	timedOut := waitErr != nil && waitCtx.Err() != nil && ctx.Err() == nil
	if timedOut {
		_, _ = c.conn.Request(context.WithoutCancel(ctx), "terminal/kill", id)
	}

	output, exit := c.terminalOutput(context.WithoutCancel(ctx), id)
	switch {
	case ctx.Err() != nil:
		return output, true, ctx.Err()
	case timedOut:
		return output, true, fmt.Errorf("command timed out after %s (terminal killed)", timeout)
	case waitErr != nil:
		return output, true, waitErr
	case exit != nil && exit.ExitCode != nil && *exit.ExitCode != 0:
		return output, true, fmt.Errorf("exit status %d", *exit.ExitCode)
	case exit != nil && exit.Signal != nil && *exit.Signal != "":
		return output, true, fmt.Errorf("terminated by signal %s", *exit.Signal)
	}
	return output, true, nil
}

func (c *clientIO) terminalOutput(ctx context.Context, id TerminalIDParams) (string, *TerminalExitStatus) {
	raw, err := c.conn.Request(ctx, "terminal/output", id)
	if err != nil {
		return "", nil
	}
	var res TerminalOutputResult
	if json.Unmarshal(raw, &res) != nil {
		return "", nil
	}
	out := res.Output
	if res.Truncated {
		out += "\n…(output truncated by the client terminal)"
	}
	return out, res.ExitStatus
}
