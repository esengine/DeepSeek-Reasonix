package plugin

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"testing"
	"time"
)

// captureWriteCloser records everything written to it, standing in for the
// transport's stdin pipe.
type captureWriteCloser struct{ b bytes.Buffer }

func (c *captureWriteCloser) Write(p []byte) (int, error) { return c.b.Write(p) }
func (c *captureWriteCloser) Close() error                { return nil }

func TestReadStdioFrameNDJSON(t *testing.T) {
	body := `{"jsonrpc":"2.0","id":1,"result":{}}`
	r := bufio.NewReader(strings.NewReader(body + "\n"))
	got, err := readStdioFrame(r)
	if err != nil {
		t.Fatalf("readStdioFrame: %v", err)
	}
	if string(got) != body {
		t.Fatalf("payload = %q, want %q", got, body)
	}
}

func TestReadStdioFrameContentLength(t *testing.T) {
	body := `{"jsonrpc":"2.0","id":2,"result":{}}`
	r := bufio.NewReader(strings.NewReader(fmt.Sprintf("Content-Length: %d\r\n\r\n%s", len(body), body)))
	got, err := readStdioFrame(r)
	if err != nil {
		t.Fatalf("readStdioFrame: %v", err)
	}
	if string(got) != body {
		t.Fatalf("payload = %q, want %q", got, body)
	}
}

func TestReadStdioFrameContentLengthCaseInsensitiveAndExtraHeaders(t *testing.T) {
	body := `{"jsonrpc":"2.0","id":3,"result":{}}`
	// Lower-case header plus an extra Content-Type line, in the order the
	// official SDKs emit them.
	raw := fmt.Sprintf("content-length: %d\r\ncontent-type: application/json\r\n\r\n%s", len(body), body)
	r := bufio.NewReader(strings.NewReader(raw))
	got, err := readStdioFrame(r)
	if err != nil {
		t.Fatalf("readStdioFrame: %v", err)
	}
	if string(got) != body {
		t.Fatalf("payload = %q, want %q", got, body)
	}
}

func TestReadStdioFrameSkipsBlankLinesBetweenFrames(t *testing.T) {
	body := `{"jsonrpc":"2.0","id":4,"result":{}}`
	raw := "\r\n" + fmt.Sprintf("Content-Length: %d\r\n\r\n%s\r\n", len(body), body)
	r := bufio.NewReader(strings.NewReader(raw))
	got, err := readStdioFrame(r)
	if err != nil {
		t.Fatalf("readStdioFrame: %v", err)
	}
	if string(got) != body {
		t.Fatalf("payload = %q, want %q", got, body)
	}
}

// The payload must arrive intact even when the underlying reader hands it over
// in tiny pieces — bufio must never mix frame boundaries.
func TestReadStdioFrameBodyAcrossBufferRefills(t *testing.T) {
	body := fmt.Sprintf(`{"jsonrpc":"2.0","id":5,"result":{"text":%q}}`, strings.Repeat("x", 200))
	raw := fmt.Sprintf("Content-Length: %d\r\n\r\n%s", len(body), body)
	r := bufio.NewReaderSize(strings.NewReader(raw), 7)
	got, err := readStdioFrame(r)
	if err != nil {
		t.Fatalf("readStdioFrame: %v", err)
	}
	if string(got) != body {
		t.Fatalf("payload = %q, want %q", got, body)
	}
}

func TestReadStdioFrameRejectsOversizedBody(t *testing.T) {
	r := bufio.NewReader(strings.NewReader(fmt.Sprintf("Content-Length: %d\r\n\r\n", maxStdioFrameBytes+1)))
	_, err := readStdioFrame(r)
	if err == nil || !strings.Contains(err.Error(), "limit is") {
		t.Fatalf("err = %v, want oversized-frame error", err)
	}
}

func TestReadStdioFrameMalformedContentLength(t *testing.T) {
	r := bufio.NewReader(strings.NewReader("Content-Length: lots\r\n\r\n{}"))
	_, err := readStdioFrame(r)
	if err == nil || !strings.Contains(err.Error(), "malformed Content-Length") {
		t.Fatalf("err = %v, want malformed Content-Length error", err)
	}
}

func TestReadStdioFrameEOF(t *testing.T) {
	r := bufio.NewReader(strings.NewReader(""))
	_, err := readStdioFrame(r)
	if err != io.EOF {
		t.Fatalf("err = %v, want io.EOF", err)
	}
}

// The transport must write standard MCP/LSP frames: a Content-Length header
// whose value exactly matches the JSON body that follows.
func TestStdioWriteUsesContentLengthFraming(t *testing.T) {
	pipe := &captureWriteCloser{}
	tr := &stdioTransport{name: "w", stdin: pipe, stderr: &tailBuffer{limit: 1024}}
	if err := tr.write(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "ping"}); err != nil {
		t.Fatalf("write: %v", err)
	}
	raw := pipe.b.String()
	sep := strings.Index(raw, "\r\n\r\n")
	if sep < 0 {
		t.Fatalf("no header/body separator in %q", raw)
	}
	header, body := raw[:sep], raw[sep+4:]
	if !strings.HasPrefix(header, "Content-Length: ") {
		t.Fatalf("header = %q, want Content-Length", header)
	}
	n, err := strconv.Atoi(strings.TrimPrefix(header, "Content-Length: "))
	if err != nil || n != len(body) {
		t.Fatalf("header %q does not match %d body bytes", header, len(body))
	}
	var msg map[string]any
	if err := json.Unmarshal([]byte(body), &msg); err != nil {
		t.Fatalf("body is not JSON: %v", err)
	}
	if msg["method"] != "ping" {
		t.Fatalf("method = %v, want ping", msg["method"])
	}
}

// End-to-end reproduction of the #7525 failure: a server that speaks only the
// standard Content-Length framing (as the official Python/Node MCP SDKs do)
// must complete the initialize handshake instead of timing out.
func TestStdioInitializeWithContentLengthServer(t *testing.T) {
	serverReads, clientWrites := io.Pipe()
	clientReads, serverWrites := io.Pipe()
	t.Cleanup(func() {
		_ = clientWrites.Close()
		_ = serverReads.Close()
		_ = serverWrites.Close()
		_ = clientReads.Close()
	})

	tr := &stdioTransport{
		name:    "sdk-server",
		stdin:   clientWrites,
		stdout:  bufio.NewReader(clientReads),
		stderr:  &tailBuffer{limit: 1024},
		pending: map[int]chan rpcResponse{},
	}
	go tr.readLoop()

	serverDone := make(chan error, 1)
	go func() {
		br := bufio.NewReader(serverReads)
		var req struct {
			ID     int    `json:"id"`
			Method string `json:"method"`
		}
		if err := decodeServerMessage(br, &req); err != nil {
			serverDone <- fmt.Errorf("read initialize: %w", err)
			return
		}
		if req.Method != "initialize" {
			serverDone <- fmt.Errorf("method = %q, want initialize", req.Method)
			return
		}
		body, err := json.Marshal(map[string]any{
			"jsonrpc": "2.0",
			"id":      req.ID,
			"result": map[string]any{
				"protocolVersion": protocolVersion,
				"serverInfo":      map[string]any{"name": "sdk-server", "version": "1.0.0"},
				"capabilities":    map[string]any{},
			},
		})
		if err != nil {
			serverDone <- err
			return
		}
		if _, err := fmt.Fprintf(serverWrites, "Content-Length: %d\r\n\r\n%s", len(body), body); err != nil {
			serverDone <- err
			return
		}
		// Like a real SDK server, keep reading: the client sends the
		// notifications/initialized notice after the handshake, and io.Pipe
		// writes block until someone consumes them.
		var initialized struct {
			Method string `json:"method"`
		}
		if err := decodeServerMessage(br, &initialized); err != nil {
			serverDone <- fmt.Errorf("read initialized notice: %w", err)
			return
		}
		if initialized.Method != "notifications/initialized" {
			serverDone <- fmt.Errorf("notice method = %q, want notifications/initialized", initialized.Method)
			return
		}
		serverDone <- nil
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	client := &Client{name: "sdk-server", t: tr, spec: Spec{WorkspaceRoot: t.TempDir()}}
	if err := client.initialize(ctx); err != nil {
		t.Fatalf("initialize with Content-Length server: %v", err)
	}
	if err := <-serverDone; err != nil {
		t.Fatal(err)
	}
}
