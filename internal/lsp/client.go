package lsp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"

	"reasonix/internal/proc"
	"reasonix/internal/secrets"
)

// docState tracks what we last sent the server for a document, so ensureSynced
// can detect an out-of-band disk edit (any tool, including bash) by stat alone.
type docState struct {
	version int
	size    int64
	mod     time.Time
}

type client struct {
	cmd    *exec.Cmd
	conn   *conn
	root   string
	langID string
	posEnc string

	mu      sync.Mutex
	docs    map[string]*docState
	diags   map[string][]Diagnostic
	diagVer map[string]int
}

// Diagnostic is one published problem for a document.
type Diagnostic struct {
	Range    Range  `json:"range"`
	Severity int    `json:"severity"`
	Message  string `json:"message"`
	Source   string `json:"source"`
}

func startClient(ctx context.Context, bin string, args []string, env map[string]string, langID, root string) (*client, error) {
	cmd := exec.CommandContext(ctx, bin, args...)
	proc.HideWindow(cmd)
	cmd.Dir = root
	cmd.Env = append(secrets.ProcessEnv(), envSlice(env)...)
	cmd.Stderr = io.Discard

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}

	c := &client{
		cmd:     cmd,
		root:    root,
		langID:  langID,
		docs:    map[string]*docState{},
		diags:   map[string][]Diagnostic{},
		diagVer: map[string]int{},
	}
	c.conn = newConn(stdin, stdout, c.handleNotify, c.handleRequest)
	initCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	if err := c.initialize(initCtx); err != nil {
		c.close()
		return nil, err
	}
	return c, nil
}

func (c *client) initialize(ctx context.Context) error {
	params := map[string]any{
		"processId": os.Getpid(),
		"rootUri":   pathToURI(c.root),
		"capabilities": map[string]any{
			"general": map[string]any{
				"positionEncodings": []string{encodingUTF8, encodingUTF16},
			},
			"textDocument": map[string]any{
				"publishDiagnostics": map[string]any{"versionSupport": true},
				"hover":              map[string]any{"contentFormat": []string{"plaintext", "markdown"}},
			},
		},
	}
	res, err := c.conn.call(ctx, "initialize", params)
	if err != nil {
		return err
	}
	var r struct {
		Capabilities struct {
			PositionEncoding string `json:"positionEncoding"`
		} `json:"capabilities"`
	}
	_ = json.Unmarshal(res, &r)
	c.posEnc = r.Capabilities.PositionEncoding
	if c.posEnc == "" {
		c.posEnc = encodingUTF16
	}
	return c.conn.notify("initialized", map[string]any{})
}

// handleNotify caches diagnostics. The version guards waitDiagnostics against
// returning problems computed for pre-edit content.
func (c *client) handleNotify(method string, params json.RawMessage) {
	if method != "textDocument/publishDiagnostics" {
		return
	}
	var p struct {
		URI         string       `json:"uri"`
		Version     *int         `json:"version"`
		Diagnostics []Diagnostic `json:"diagnostics"`
	}
	if json.Unmarshal(params, &p) != nil {
		return
	}
	c.mu.Lock()
	c.diags[p.URI] = p.Diagnostics
	if p.Version != nil {
		c.diagVer[p.URI] = *p.Version
	} else if d := c.docs[p.URI]; d != nil {
		c.diagVer[p.URI] = d.version
	}
	c.mu.Unlock()
}

// handleRequest answers the server→client requests that block initialization on
// some servers (rust-analyzer stalls without a workspace/configuration reply).
func (c *client) handleRequest(id int64, method string, params json.RawMessage) {
	switch method {
	case "workspace/configuration":
		var p struct {
			Items []json.RawMessage `json:"items"`
		}
		_ = json.Unmarshal(params, &p)
		_ = c.conn.reply(id, make([]any, len(p.Items)))
	default:
		_ = c.conn.reply(id, nil)
	}
}

func (c *client) ensureSynced(uri, path string) error {
	fi, err := os.Stat(path)
	if err != nil {
		return err
	}
	c.mu.Lock()
	d, open := c.docs[uri]
	c.mu.Unlock()
	if open && fi.Size() == d.size && fi.ModTime().Equal(d.mod) {
		return nil
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if !open {
		err = c.conn.notify("textDocument/didOpen", map[string]any{
			"textDocument": map[string]any{
				"uri": uri, "languageId": c.langID, "version": 1, "text": string(content),
			},
		})
		c.mu.Lock()
		c.docs[uri] = &docState{version: 1, size: fi.Size(), mod: fi.ModTime()}
		c.mu.Unlock()
		return err
	}
	ver := d.version + 1
	err = c.conn.notify("textDocument/didChange", map[string]any{
		"textDocument":   map[string]any{"uri": uri, "version": ver},
		"contentChanges": []any{map[string]any{"text": string(content)}},
	})
	c.mu.Lock()
	c.docs[uri] = &docState{version: ver, size: fi.Size(), mod: fi.ModTime()}
	c.mu.Unlock()
	return err
}

func (c *client) docVersion(uri string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	if d := c.docs[uri]; d != nil {
		return d.version
	}
	return 0
}

// waitDiagnostics blocks until a publishDiagnostics for uri at version >= minVer
// arrives or the deadline elapses, returning the freshest cache either way.
func (c *client) waitDiagnostics(ctx context.Context, uri string, minVer int, deadline time.Duration) []Diagnostic {
	end := time.Now().Add(deadline)
	for {
		c.mu.Lock()
		ver, d := c.diagVer[uri], c.diags[uri]
		c.mu.Unlock()
		if ver >= minVer || time.Now().After(end) {
			return d
		}
		select {
		case <-ctx.Done():
			return d
		case <-time.After(40 * time.Millisecond):
		}
	}
}

// callRetry retries a request while the server answers ContentModified (-32801),
// which means it is mid-reindex and the state is in flux. A short bounded retry
// hides the brief window after a didOpen/didChange; a longer reindex still
// surfaces so the caller can decide (see Manager, which turns it into a
// retry-shortly message).
func (c *client) callRetry(ctx context.Context, method string, params any) (json.RawMessage, error) {
	const attempts = 5
	for i := 0; ; i++ {
		raw, err := c.conn.call(ctx, method, params)
		if err == nil || i >= attempts || !isContentModified(err) {
			return raw, err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(400 * time.Millisecond):
		}
	}
}

func isContentModified(err error) bool {
	var e *rpcError
	return errors.As(err, &e) && e.Code == -32801
}

func (c *client) query(ctx context.Context, method, uri string, pos Position) (json.RawMessage, error) {
	return c.callRetry(ctx, method, map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     pos,
	})
}

func (c *client) references(ctx context.Context, uri string, pos Position) (json.RawMessage, error) {
	return c.callRetry(ctx, "textDocument/references", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     pos,
		"context":      map[string]any{"includeDeclaration": true},
	})
}

func (c *client) close() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, _ = c.conn.call(ctx, "shutdown", nil)
	_ = c.conn.notify("exit", nil)
	if c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
	}
	_ = c.cmd.Wait()
}

func envSlice(env map[string]string) []string {
	out := make([]string, 0, len(env))
	for k, v := range env {
		out = append(out, k+"="+v)
	}
	return out
}
