package acp

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"
)

type acpCompatWireFrame struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *rpcError       `json:"error"`
}

func serveACPCompatWire(t *testing.T, input string, register func(*Conn)) (acpCompatWireFrame, error) {
	t.Helper()
	var out bytes.Buffer
	conn := NewConn(strings.NewReader(input), &out)
	if register != nil {
		register(conn)
	}
	err := conn.Serve(context.Background())
	var got acpCompatWireFrame
	if decodeErr := json.Unmarshal(bytes.TrimSpace(out.Bytes()), &got); decodeErr != nil {
		t.Fatalf("decode ACP frame %q: %v", out.String(), decodeErr)
	}
	return got, err
}

func TestACPConnErrorWireCompatibility(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantID  string
		wantErr int
	}{
		{
			name:    "parse error",
			input:   "{not-json}\n",
			wantID:  "null",
			wantErr: ErrParse,
		},
		{
			name:    "invalid request",
			input:   "{\"jsonrpc\":\"2.0\",\"params\":{}}\n",
			wantID:  "null",
			wantErr: ErrInvalidRequest,
		},
		{
			name:    "unknown method",
			input:   "{\"jsonrpc\":\"2.0\",\"id\":\"req-7\",\"method\":\"missing/method\",\"params\":{}}\n",
			wantID:  `"req-7"`,
			wantErr: ErrMethodNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := serveACPCompatWire(t, tt.input, nil)
			if err != nil {
				t.Fatalf("Serve: %v", err)
			}
			if got.JSONRPC != "2.0" {
				t.Fatalf("jsonrpc = %q, want 2.0", got.JSONRPC)
			}
			if string(got.ID) != tt.wantID {
				t.Fatalf("id = %s, want %s", got.ID, tt.wantID)
			}
			if got.Error == nil || got.Error.Code != tt.wantErr {
				t.Fatalf("error = %+v, want code %d", got.Error, tt.wantErr)
			}
		})
	}
}

func TestACPConnRemainsPermissiveWhenStrictModeIsOff(t *testing.T) {
	got, err := serveACPCompatWire(t,
		"{\"id\":7,\"method\":\"legacy/echo\",\"params\":\"scalar\"}\n",
		func(conn *Conn) {
			conn.Handle("legacy/echo", func(_ context.Context, params json.RawMessage) (any, error) {
				return map[string]json.RawMessage{"params": params}, nil
			})
		},
	)
	if err != nil {
		t.Fatalf("Serve: %v", err)
	}
	if got.Error != nil {
		t.Fatalf("unexpected error: %+v", got.Error)
	}
	if string(got.ID) != "7" {
		t.Fatalf("id = %s, want 7", got.ID)
	}
	var result struct {
		Params string `json:"params"`
	}
	if err := json.Unmarshal(got.Result, &result); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if result.Params != "scalar" {
		t.Fatalf("params = %q, want scalar", result.Params)
	}
}

func TestACPConnNotificationHasNoResponse(t *testing.T) {
	called := make(chan json.RawMessage, 1)
	var out bytes.Buffer
	conn := NewConn(strings.NewReader("{\"jsonrpc\":\"2.0\",\"method\":\"note\",\"params\":{\"n\":1}}\n"), &out)
	conn.HandleNotify("note", func(_ context.Context, params json.RawMessage) {
		called <- append(json.RawMessage(nil), params...)
	})
	if err := conn.Serve(context.Background()); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	select {
	case params := <-called:
		if string(params) != `{"n":1}` {
			t.Fatalf("params = %s", params)
		}
	default:
		t.Fatal("notification handler was not called")
	}
	if out.Len() != 0 {
		t.Fatalf("notification produced response %q", out.String())
	}
}

func TestACPConnCancelsRequestHandlerOnDisconnect(t *testing.T) {
	inR, inW := io.Pipe()
	var out bytes.Buffer
	conn := NewConn(inR, &out)
	started := make(chan struct{})
	cancelled := make(chan error, 1)
	conn.Handle("block", func(ctx context.Context, _ json.RawMessage) (any, error) {
		close(started)
		<-ctx.Done()
		cancelled <- ctx.Err()
		return nil, ctx.Err()
	})

	done := make(chan error, 1)
	go func() { done <- conn.Serve(context.Background()) }()
	if _, err := io.WriteString(inW, "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"block\",\"params\":{}}\n"); err != nil {
		t.Fatalf("write request: %v", err)
	}
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("request handler did not start")
	}
	if err := inW.Close(); err != nil {
		t.Fatalf("close input: %v", err)
	}

	select {
	case err := <-cancelled:
		if err != context.Canceled {
			t.Fatalf("handler context error = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("disconnect did not cancel request handler")
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Serve: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Serve did not return after handler cancellation")
	}
}

func TestACPConnRetains32MiBInboundAllowance(t *testing.T) {
	if maxMessageBytes != 32<<20 {
		t.Fatalf("maxMessageBytes = %d, want %d", maxMessageBytes, 32<<20)
	}
}
