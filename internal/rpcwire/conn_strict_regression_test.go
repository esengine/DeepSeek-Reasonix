package rpcwire

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type strictTestResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *ErrorObject    `json:"error"`
}

func decodeStrictTestResponses(t *testing.T, raw []byte) []strictTestResponse {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(raw))
	var frames []strictTestResponse
	for {
		var frame strictTestResponse
		if err := dec.Decode(&frame); errors.Is(err, io.EOF) {
			return frames
		} else if err != nil {
			t.Fatalf("decode response %d from %q: %v", len(frames), raw, err)
		}
		frames = append(frames, frame)
	}
}

func TestEOFDelimitedFinalFrameIsDispatched(t *testing.T) {
	t.Run("complete request without newline", func(t *testing.T) {
		var out bytes.Buffer
		conn := NewConn(strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"echo","params":{"v":1}}`), &out, Options{StrictJSONRPC: true})
		conn.Handle("echo", func(_ context.Context, params json.RawMessage) (any, error) {
			return json.RawMessage(params), nil
		})
		if err := conn.Serve(context.Background()); err != nil {
			t.Fatalf("Serve: %v", err)
		}
		frames := decodeStrictTestResponses(t, out.Bytes())
		if len(frames) != 1 || frames[0].Error != nil || string(frames[0].Result) != `{"v":1}` {
			t.Fatalf("frames = %+v, raw = %q", frames, out.String())
		}
	})

	t.Run("truncated JSON without newline", func(t *testing.T) {
		var out bytes.Buffer
		conn := NewConn(strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"echo"`), &out, Options{StrictJSONRPC: true})
		if err := conn.Serve(context.Background()); err != nil {
			t.Fatalf("Serve: %v", err)
		}
		frames := decodeStrictTestResponses(t, out.Bytes())
		if len(frames) != 1 || frames[0].Error == nil || frames[0].Error.Code != ErrParse || string(frames[0].ID) != "null" {
			t.Fatalf("frames = %+v, raw = %q", frames, out.String())
		}
	})
}

func TestInboundLimitAcceptsFrameAtExactBoundary(t *testing.T) {
	frame := "{\"jsonrpc\":\"2.0\",\"method\":\"note\",\"params\":{}}\n"
	var called atomic.Int32
	conn := NewConn(strings.NewReader(frame), io.Discard, Options{MaxInboundBytes: len(frame), StrictJSONRPC: true})
	conn.HandleNotify("note", func(context.Context, json.RawMessage) { called.Add(1) })
	if err := conn.Serve(context.Background()); err != nil {
		t.Fatalf("Serve at exact boundary: %v", err)
	}
	if got := called.Load(); got != 1 {
		t.Fatalf("notification calls = %d, want 1", got)
	}
}

type busyResponseWriter struct {
	mu   sync.Mutex
	buf  bytes.Buffer
	busy chan struct{}
}

func (w *busyResponseWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	n, err := w.buf.Write(p)
	if bytes.Contains(p, []byte(`"message":"server busy"`)) {
		select {
		case w.busy <- struct{}{}:
		default:
		}
	}
	return n, err
}

func (w *busyResponseWriter) Bytes() []byte {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]byte(nil), w.buf.Bytes()...)
}

func TestInboundHandlerConcurrencyIsBoundedWithoutBlockingResponses(t *testing.T) {
	var input strings.Builder
	for id := 1; id <= 5; id++ {
		fmt.Fprintf(&input, "{\"jsonrpc\":\"2.0\",\"id\":%d,\"method\":\"block\",\"params\":{}}\n", id)
	}
	out := &busyResponseWriter{busy: make(chan struct{}, 3)}
	release := make(chan struct{})
	started := make(chan struct{}, 2)
	var running atomic.Int32
	var maximum atomic.Int32
	conn := NewConn(strings.NewReader(input.String()), out, Options{
		StrictJSONRPC: true, MaxConcurrentHandlers: 2,
	})
	conn.Handle("block", func(context.Context, json.RawMessage) (any, error) {
		current := running.Add(1)
		for {
			observed := maximum.Load()
			if current <= observed || maximum.CompareAndSwap(observed, current) {
				break
			}
		}
		started <- struct{}{}
		<-release
		running.Add(-1)
		return struct{}{}, nil
	})
	done := make(chan error, 1)
	go func() { done <- conn.Serve(context.Background()) }()
	for i := 0; i < 2; i++ {
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("bounded handlers did not start")
		}
	}
	for i := 0; i < 3; i++ {
		select {
		case <-out.busy:
		case <-time.After(time.Second):
			t.Fatal("overload responses were blocked behind active handlers")
		}
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if got := maximum.Load(); got != 2 {
		t.Fatalf("maximum concurrent handlers = %d, want 2", got)
	}
	raw := out.Bytes()
	frames := decodeStrictTestResponses(t, raw)
	busy := 0
	for _, frame := range frames {
		if frame.Error != nil && frame.Error.Code == ErrServerBusy && frame.Error.Message == "server busy" {
			busy++
		}
	}
	if len(frames) != 5 || busy != 3 {
		t.Fatalf("responses=%d busy=%d, want 5/3; raw=%q", len(frames), busy, raw)
	}
}

func TestQueuedNotificationsPreserveBurstOrder(t *testing.T) {
	const count = 500
	var input strings.Builder
	for i := 0; i < count; i++ {
		fmt.Fprintf(&input, "{\"jsonrpc\":\"2.0\",\"method\":\"note\",\"params\":{\"index\":%d}}\n", i)
	}
	conn := NewConn(strings.NewReader(input.String()), io.Discard, Options{
		Name: "ordered-notifications", StrictJSONRPC: true, MaxQueuedNotifications: count,
	})
	got := make([]int, 0, count)
	conn.HandleNotify("note", func(_ context.Context, params json.RawMessage) {
		var value struct {
			Index int `json:"index"`
		}
		if err := json.Unmarshal(params, &value); err != nil {
			t.Errorf("decode notification: %v", err)
			return
		}
		got = append(got, value.Index)
	})
	if err := conn.Serve(context.Background()); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	if len(got) != count {
		t.Fatalf("notification calls = %d, want %d", len(got), count)
	}
	for i, value := range got {
		if value != i {
			t.Fatalf("notification[%d] = %d, want %d", i, value, i)
		}
	}
}

func TestQueuedNotificationOverflowFailsConnection(t *testing.T) {
	var input strings.Builder
	for i := 0; i < 100; i++ {
		fmt.Fprintf(&input, "{\"jsonrpc\":\"2.0\",\"method\":\"note\",\"params\":{\"index\":%d}}\n", i)
	}
	conn := NewConn(strings.NewReader(input.String()), io.Discard, Options{
		Name: "notification-overflow", StrictJSONRPC: true, MaxQueuedNotifications: 1,
	})
	conn.HandleNotify("note", func(ctx context.Context, _ json.RawMessage) {
		<-ctx.Done()
	})
	err := conn.Serve(context.Background())
	if err == nil || !strings.Contains(err.Error(), "notification-overflow: notification queue overflow") {
		t.Fatalf("Serve error = %v, want notification queue overflow", err)
	}
}

func TestStrictJSONRPCAcceptsLegalRequestNotificationAndResponses(t *testing.T) {
	t.Run("request and notification", func(t *testing.T) {
		input := strings.Join([]string{
			`{"jsonrpc":"2.0","id":1,"method":"sum","params":[2,3]}`,
			`{"jsonrpc":"2.0","method":"note","params":{"ok":true}}`,
		}, "\n") + "\n"
		var out bytes.Buffer
		conn := NewConn(strings.NewReader(input), &out, Options{StrictJSONRPC: true})
		var requestCalls atomic.Int32
		var notificationCalls atomic.Int32
		conn.Handle("sum", func(_ context.Context, params json.RawMessage) (any, error) {
			requestCalls.Add(1)
			if string(params) != `[2,3]` {
				t.Errorf("request params = %s", params)
			}
			return map[string]int{"sum": 5}, nil
		})
		conn.HandleNotify("note", func(_ context.Context, params json.RawMessage) {
			notificationCalls.Add(1)
			if string(params) != `{"ok":true}` {
				t.Errorf("notification params = %s", params)
			}
		})
		if err := conn.Serve(context.Background()); err != nil {
			t.Fatalf("Serve: %v", err)
		}
		if requestCalls.Load() != 1 || notificationCalls.Load() != 1 {
			t.Fatalf("request calls = %d, notification calls = %d", requestCalls.Load(), notificationCalls.Load())
		}
		frames := decodeStrictTestResponses(t, out.Bytes())
		if len(frames) != 1 || frames[0].Error != nil || string(frames[0].Result) != `{"sum":5}` {
			t.Fatalf("frames = %+v, raw = %q", frames, out.String())
		}
	})

	for _, tt := range []struct {
		name      string
		frame     string
		want      string
		wantError int
	}{
		{name: "result response", frame: `{"jsonrpc":"2.0","id":9,"result":{"ok":true}}`, want: `{"ok":true}`},
		{name: "error response", frame: `{"jsonrpc":"2.0","id":9,"error":{"code":-32007,"message":"busy","data":{"retry":true}}}`, wantError: -32007},
	} {
		t.Run(tt.name, func(t *testing.T) {
			conn := NewConn(strings.NewReader(tt.frame), io.Discard, Options{StrictJSONRPC: true})
			ch := make(chan rpcResult, 1)
			conn.pending[9] = ch
			if err := conn.Serve(context.Background()); err != nil {
				t.Fatalf("Serve: %v", err)
			}
			result := <-ch
			if tt.wantError != 0 {
				var responseErr *ResponseError
				if !errors.As(result.err, &responseErr) || responseErr.Code != tt.wantError || !bytes.Contains(responseErr.Data, []byte(`"retry":true`)) {
					t.Fatalf("response error = %#v", result.err)
				}
				return
			}
			if result.err != nil || string(result.result) != tt.want {
				t.Fatalf("result = %s, error = %v", result.result, result.err)
			}
		})
	}
}

func TestStrictJSONRPCRejectsResultAndErrorBadVersionAndScalarParams(t *testing.T) {
	tests := []struct {
		name  string
		frame string
	}{
		{
			name:  "response has result and error",
			frame: `{"jsonrpc":"2.0","id":1,"result":{},"error":{"code":-1,"message":"bad"}}`,
		},
		{
			name:  "wrong jsonrpc version",
			frame: `{"jsonrpc":"1.0","id":1,"method":"run","params":{}}`,
		},
		{
			name:  "string params",
			frame: `{"jsonrpc":"2.0","id":1,"method":"run","params":"bad"}`,
		},
		{
			name:  "number params",
			frame: `{"jsonrpc":"2.0","id":1,"method":"run","params":7}`,
		},
		{
			name:  "null params",
			frame: `{"jsonrpc":"2.0","id":1,"method":"run","params":null}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			conn := NewConn(strings.NewReader(tt.frame+"\n"), &out, Options{StrictJSONRPC: true})
			var called atomic.Int32
			conn.Handle("run", func(context.Context, json.RawMessage) (any, error) {
				called.Add(1)
				return nil, nil
			})
			if err := conn.Serve(context.Background()); err != nil {
				t.Fatalf("Serve: %v", err)
			}
			if called.Load() != 0 {
				t.Fatalf("handler called %d times", called.Load())
			}
			frames := decodeStrictTestResponses(t, out.Bytes())
			if len(frames) != 1 || frames[0].Error == nil || frames[0].Error.Code != ErrInvalidRequest || string(frames[0].ID) != "1" {
				t.Fatalf("frames = %+v, raw = %q", frames, out.String())
			}
		})
	}
}

type oneByteWriter struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (w *oneByteWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(p) == 0 {
		return 0, nil
	}
	_ = w.buf.WriteByte(p[0])
	runtime.Gosched()
	return 1, nil
}

func (w *oneByteWriter) Bytes() []byte {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]byte(nil), w.buf.Bytes()...)
}

func TestConcurrentWritesDoNotInterleaveFrames(t *testing.T) {
	const count = 64
	w := &oneByteWriter{}
	conn := NewConn(strings.NewReader(""), w, Options{})
	var wg sync.WaitGroup
	errs := make(chan error, count)
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			errs <- conn.Notify("event", map[string]int{"index": i})
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("Notify: %v", err)
		}
	}

	lines := bytes.Split(bytes.TrimSpace(w.Bytes()), []byte{'\n'})
	if len(lines) != count {
		t.Fatalf("frame count = %d, want %d", len(lines), count)
	}
	seen := make(map[int]bool, count)
	for i, line := range lines {
		var frame struct {
			JSONRPC string `json:"jsonrpc"`
			Method  string `json:"method"`
			Params  struct {
				Index int `json:"index"`
			} `json:"params"`
		}
		if err := json.Unmarshal(line, &frame); err != nil {
			t.Fatalf("frame %d is interleaved or invalid (%q): %v", i, line, err)
		}
		if frame.JSONRPC != "2.0" || frame.Method != "event" {
			t.Fatalf("frame %d = %+v", i, frame)
		}
		if seen[frame.Params.Index] {
			t.Fatalf("duplicate index %d", frame.Params.Index)
		}
		seen[frame.Params.Index] = true
	}
	if len(seen) != count {
		t.Fatalf("unique payloads = %d, want %d", len(seen), count)
	}
}

type failWriter struct{ err error }

func (w failWriter) Write([]byte) (int, error) { return 0, w.err }

func TestHandlerResponseWriteFailureTerminatesConnection(t *testing.T) {
	wantErr := errors.New("write failed")
	request := "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"ok\",\"params\":{}}\n"
	conn := NewConn(strings.NewReader(request), failWriter{err: wantErr}, Options{})
	conn.Handle("ok", func(context.Context, json.RawMessage) (any, error) {
		return map[string]bool{"ok": true}, nil
	})
	if err := conn.Serve(context.Background()); !errors.Is(err, wantErr) {
		t.Fatalf("Serve error = %v, want %v", err, wantErr)
	}
}

func TestOversizedHandlerResultFailsConnectionWhenErrorCannotFit(t *testing.T) {
	request := "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"large\",\"params\":{}}\n"
	var out bytes.Buffer
	conn := NewConn(strings.NewReader(request), &out, Options{MaxOutboundBytes: 24})
	conn.Handle("large", func(context.Context, json.RawMessage) (any, error) {
		return map[string]string{"body": strings.Repeat("x", 1024)}, nil
	})
	err := conn.Serve(context.Background())
	var tooLarge *FrameTooLargeError
	if !errors.As(err, &tooLarge) || tooLarge.Direction != "outbound" || tooLarge.Limit != 24 {
		t.Fatalf("Serve error = %v", err)
	}
	if out.Len() != 0 {
		t.Fatalf("wrote oversized fallback response %q", out.String())
	}
}

func TestRequestWriteFailureReturnsOriginalError(t *testing.T) {
	wantErr := errors.New("request write failed")
	conn := NewConn(strings.NewReader(""), failWriter{err: wantErr}, Options{Name: "test"})
	_, err := conn.Request(context.Background(), "call", map[string]string{"v": fmt.Sprint(1)})
	if !errors.Is(err, wantErr) {
		t.Fatalf("Request error = %v, want %v", err, wantErr)
	}
}

func TestRequestStartedAfterServeEOFReturnsClosed(t *testing.T) {
	conn := NewConn(strings.NewReader(""), io.Discard, Options{Name: "closed-race"})
	done := make(chan error, 1)
	go func() { done <- conn.Serve(context.Background()) }()
	if err := <-done; err != nil {
		t.Fatalf("Serve: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err := conn.Request(ctx, "late", struct{}{})
	if err == nil || errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("late Request error = %v, want immediate connection closed", err)
	}
}
