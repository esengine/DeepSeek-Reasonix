package rpcwire

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestStructuredHandlerError(t *testing.T) {
	in := strings.NewReader("{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"fail\",\"params\":{}}\n")
	var out bytes.Buffer
	conn := NewConn(in, &out, Options{MaxInboundBytes: 1024, MaxOutboundBytes: 1024})
	conn.Handle("fail", func(context.Context, json.RawMessage) (any, error) {
		return nil, &RPCError{Code: -32000, Message: "controlled", Data: map[string]any{"reasonixCode": "HOST_BUSY", "retryable": true}}
	})
	if err := conn.Serve(context.Background()); err != nil {
		t.Fatal(err)
	}
	var frame struct {
		Error *ErrorObject `json:"error"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(out.Bytes()), &frame); err != nil {
		t.Fatal(err)
	}
	if frame.Error == nil || frame.Error.Code != -32000 || frame.Error.Message != "controlled" {
		t.Fatalf("error = %+v", frame.Error)
	}
	var data map[string]any
	if err := json.Unmarshal(frame.Error.Data, &data); err != nil {
		t.Fatal(err)
	}
	if data["reasonixCode"] != "HOST_BUSY" || data["retryable"] != true {
		t.Fatalf("data = %#v", data)
	}
}

func TestHandlerResponseAfterWriteRunsAfterSuccessfulFrame(t *testing.T) {
	request := "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"detach\",\"params\":{}}\n"
	var out bytes.Buffer
	callback := make(chan struct {
		err      error
		response string
	}, 1)
	conn := NewConn(strings.NewReader(request), &out, Options{StrictJSONRPC: true})
	conn.Handle("detach", func(context.Context, json.RawMessage) (any, error) {
		return RespondThen(map[string]bool{"detached": true}, func(err error) {
			callback <- struct {
				err      error
				response string
			}{err: err, response: out.String()}
		}), nil
	})
	if err := conn.Serve(context.Background()); err != nil {
		t.Fatal(err)
	}
	got := <-callback
	if got.err != nil {
		t.Fatalf("callback error = %v", got.err)
	}
	if !strings.Contains(got.response, `"result":{"detached":true}`) {
		t.Fatalf("callback ran before response write: %q", got.response)
	}
}

func TestHandlerResponseAfterWriteReceivesTransportFailure(t *testing.T) {
	wantErr := errors.New("write failed")
	callback := make(chan error, 1)
	conn := NewConn(
		strings.NewReader("{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"detach\",\"params\":{}}\n"),
		failWriter{err: wantErr},
		Options{StrictJSONRPC: true},
	)
	conn.Handle("detach", func(context.Context, json.RawMessage) (any, error) {
		return RespondThen(map[string]bool{"detached": true}, func(err error) { callback <- err }), nil
	})
	if err := conn.Serve(context.Background()); !errors.Is(err, wantErr) {
		t.Fatalf("Serve error = %v, want %v", err, wantErr)
	}
	if err := <-callback; !errors.Is(err, wantErr) {
		t.Fatalf("callback error = %v, want %v", err, wantErr)
	}
}

func TestRequestKeepsDeliveredResponseWhenPeerClosesAfterWrite(t *testing.T) {
	for attempt := 0; attempt < 100; attempt++ {
		serverToClientR, serverToClientW := io.Pipe()
		clientToServerR, clientToServerW := io.Pipe()
		client := NewConn(serverToClientR, clientToServerW, Options{Name: "response-close-client"})
		server := NewConn(clientToServerR, serverToClientW, Options{Name: "response-close-server"})
		server.Handle("detach", func(context.Context, json.RawMessage) (any, error) {
			return RespondThen(map[string]bool{"detached": true}, func(error) {
				_ = serverToClientW.Close()
			}), nil
		})
		ctx, cancel := context.WithCancel(context.Background())
		clientDone := make(chan struct{})
		serverDone := make(chan struct{})
		go func() { _ = client.Serve(ctx); close(clientDone) }()
		go func() { _ = server.Serve(ctx); close(serverDone) }()
		raw, err := client.Request(ctx, "detach", struct{}{})
		cancel()
		_ = clientToServerW.Close()
		_ = serverToClientW.Close()
		<-clientDone
		<-serverDone
		if err != nil {
			t.Fatalf("attempt %d lost the written response to peer EOF: %v", attempt, err)
		}
		if !bytes.Contains(raw, []byte(`"detached":true`)) {
			t.Fatalf("attempt %d response = %s", attempt, raw)
		}
	}
}

func TestInboundLimitIncludesNewline(t *testing.T) {
	line := "{\"jsonrpc\":\"2.0\",\"method\":\"n\"}\n"
	conn := NewConn(strings.NewReader(line), io.Discard, Options{MaxInboundBytes: len(line) - 1, Name: "test"})
	err := conn.Serve(context.Background())
	var tooLarge *FrameTooLargeError
	if !errors.As(err, &tooLarge) || tooLarge.Direction != "inbound" || tooLarge.Limit != len(line)-1 {
		t.Fatalf("error = %v", err)
	}
}

func TestOutboundLimitIncludesNewline(t *testing.T) {
	var out bytes.Buffer
	conn := NewConn(strings.NewReader(""), &out, Options{MaxOutboundBytes: 8})
	err := conn.Notify("event", map[string]string{"body": "too large"})
	var tooLarge *FrameTooLargeError
	if !errors.As(err, &tooLarge) || tooLarge.Direction != "outbound" || tooLarge.Size <= tooLarge.Limit {
		t.Fatalf("error = %v", err)
	}
	if out.Len() != 0 {
		t.Fatalf("wrote %d bytes after rejecting frame", out.Len())
	}
}

func TestRequestReturnsStructuredPeerError(t *testing.T) {
	serverToClientR, serverToClientW := io.Pipe()
	clientToServerR, clientToServerW := io.Pipe()
	client := NewConn(serverToClientR, clientToServerW, Options{})
	server := NewConn(clientToServerR, serverToClientW, Options{})
	server.Handle("fail", func(context.Context, json.RawMessage) (any, error) {
		return nil, &RPCError{Code: -32000, Message: "busy", Data: map[string]any{"reasonixCode": "HOST_BUSY"}}
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = client.Serve(ctx) }()
	go func() { _ = server.Serve(ctx) }()
	_, err := client.Request(ctx, "fail", struct{}{})
	var responseErr *ResponseError
	if !errors.As(err, &responseErr) || responseErr.Code != -32000 || !bytes.Contains(responseErr.Data, []byte("HOST_BUSY")) {
		t.Fatalf("error = %#v", err)
	}
	_ = clientToServerW.Close()
	_ = serverToClientW.Close()
}

func TestStrictJSONRPCRejectsMissingVersionAndInvalidShape(t *testing.T) {
	input := strings.Join([]string{
		`{"id":1,"method":"ping","params":{}}`,
		`{"jsonrpc":"2.0","id":2,"method":"ping","result":{}}`,
		`{"jsonrpc":"2.0","id":3,"method":"ping","params":"bad"}`,
		`{"jsonrpc":"2.0","id":{},"method":"ping","params":{}}`,
		`{"jsonrpc":"2.0","id":5,"error":{"code":-32000}}`,
		`{"jsonrpc":"2.0","id":6,"error":"bad"}`,
	}, "\n") + "\n"
	var out bytes.Buffer
	conn := NewConn(strings.NewReader(input), &out, Options{StrictJSONRPC: true})
	called := 0
	conn.Handle("ping", func(context.Context, json.RawMessage) (any, error) {
		called++
		return struct{}{}, nil
	})
	if err := conn.Serve(context.Background()); err != nil {
		t.Fatal(err)
	}
	if called != 0 {
		t.Fatalf("handler called %d times", called)
	}
	dec := json.NewDecoder(&out)
	wantIDs := []string{"1", "2", "3", "null", "5", "null"}
	for i := 0; i < 6; i++ {
		var frame struct {
			ID    json.RawMessage `json:"id"`
			Error *ErrorObject    `json:"error"`
		}
		if err := dec.Decode(&frame); err != nil {
			t.Fatalf("decode response %d: %v", i, err)
		}
		if frame.Error == nil || frame.Error.Code != ErrInvalidRequest {
			t.Fatalf("response %d error = %+v", i, frame.Error)
		}
		if string(frame.ID) != wantIDs[i] {
			t.Fatalf("response %d id = %s, want %s", i, frame.ID, wantIDs[i])
		}
	}
}

func TestOversizedHandlerResultGetsSmallErrorResponse(t *testing.T) {
	request := "{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"large\",\"params\":{}}\n"
	var out bytes.Buffer
	conn := NewConn(strings.NewReader(request), &out, Options{MaxOutboundBytes: 160})
	conn.Handle("large", func(context.Context, json.RawMessage) (any, error) {
		return map[string]string{"body": strings.Repeat("x", 1024)}, nil
	})
	if err := conn.Serve(context.Background()); err != nil {
		t.Fatal(err)
	}
	var frame struct {
		Error *ErrorObject `json:"error"`
	}
	if err := json.Unmarshal(bytes.TrimSpace(out.Bytes()), &frame); err != nil {
		t.Fatal(err)
	}
	if frame.Error == nil || frame.Error.Code != ErrInternal || frame.Error.Message != "response exceeds frame size limit" {
		t.Fatalf("error = %+v", frame.Error)
	}
}

func TestBeforeRequestObservesArrivalOrderBeforeHandlersRun(t *testing.T) {
	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","id":2,"method":"business","params":{}}`,
	}, "\n") + "\n"
	var out bytes.Buffer
	state := "new"
	var stateMu sync.Mutex
	businessSeen := make(chan struct{})
	conn := NewConn(strings.NewReader(input), &out, Options{
		StrictJSONRPC: true,
		BeforeRequest: func(method string, _ json.RawMessage) error {
			stateMu.Lock()
			defer stateMu.Unlock()
			switch state {
			case "new":
				if method != "initialize" {
					return &RPCError{Code: ErrInvalidRequest, Message: "initialize must be first"}
				}
				state = "initializing"
				return nil
			case "initializing":
				if method == "business" {
					close(businessSeen)
				}
				return &RPCError{Code: ErrInvalidRequest, Message: "initialize incomplete"}
			default:
				return nil
			}
		},
	})
	started := make(chan struct{})
	release := make(chan struct{})
	businessRan := make(chan struct{}, 1)
	conn.Handle("initialize", func(context.Context, json.RawMessage) (any, error) {
		close(started)
		<-release
		stateMu.Lock()
		state = "ready"
		stateMu.Unlock()
		return struct{}{}, nil
	})
	conn.Handle("business", func(context.Context, json.RawMessage) (any, error) {
		businessRan <- struct{}{}
		return struct{}{}, nil
	})
	done := make(chan error, 1)
	go func() { done <- conn.Serve(context.Background()) }()
	<-started
	select {
	case <-businessSeen:
	case <-time.After(2 * time.Second):
		close(release)
		t.Fatal("business request did not pass through the arrival gate")
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	dec := json.NewDecoder(bytes.NewReader(out.Bytes()))
	seenRejected := false
	for {
		var frame struct {
			ID    json.RawMessage `json:"id"`
			Error *ErrorObject    `json:"error"`
		}
		if err := dec.Decode(&frame); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			t.Fatal(err)
		}
		if string(frame.ID) == "2" && frame.Error != nil && frame.Error.Message == "initialize incomplete" {
			seenRejected = true
		}
	}
	if !seenRejected {
		t.Fatalf("frames = %s", out.String())
	}
	select {
	case <-businessRan:
		t.Fatal("business handler ran before initialize completed")
	default:
	}
}

func TestBeforeNotificationSynchronouslyRejectsWithoutResponse(t *testing.T) {
	input := "{\"jsonrpc\":\"2.0\",\"method\":\"client/note\",\"params\":{}}\n"
	var out bytes.Buffer
	gateCalled := false
	handlerCalled := false
	conn := NewConn(strings.NewReader(input), &out, Options{
		StrictJSONRPC: true,
		BeforeNotification: func(method string, _ json.RawMessage) error {
			gateCalled = method == "client/note"
			return &RPCError{Code: ErrInvalidRequest, Message: "notifications forbidden"}
		},
	})
	conn.HandleNotify("client/note", func(context.Context, json.RawMessage) { handlerCalled = true })
	if err := conn.Serve(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !gateCalled || handlerCalled {
		t.Fatalf("gateCalled=%v handlerCalled=%v", gateCalled, handlerCalled)
	}
	if out.Len() != 0 {
		t.Fatalf("JSON-RPC notification rejection emitted a response: %s", out.String())
	}
}
