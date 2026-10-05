package plugin

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"sync"
	"testing"
	"time"
)

// One server, two calls: a long screening run and whatever the model issues
// alongside it. A transport that demuxes by id has no reason to queue them, and
// queueing is what turned a slow tool into a wedged host — the wait for the pipe
// took no context, so even an abandoned turn could not get its goroutine back.
func TestStdioCallsOverlapOnOneServer(t *testing.T) {
	tr, server, cleanup := stdioPipePair(t, "das")
	defer cleanup()

	// The server answers in reverse, which only works if both requests reached
	// it before either reply was written.
	served := make(chan error, 1)
	go func() {
		ids, err := server.readRequestIDs(2)
		if err != nil {
			served <- err
			return
		}
		for _, v := range slices.Backward(ids) {
			if err := server.reply(v, map[string]any{"id": v}); err != nil {
				served <- err
				return
			}
		}
		served <- nil
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i, name := range []string{"run_flow_screening", "query_model"} {
		wg.Go(func() {
			_, errs[i] = tr.call(ctx, "tools/call", map[string]any{"name": name})
		})
	}
	wg.Wait()

	if err := <-served; err != nil {
		t.Fatalf("server: %v", err)
	}
	for i, err := range errs {
		if err != nil {
			t.Errorf("call %d: %v", i, err)
		}
	}
}

// Demuxing by id is the only thing that makes overlap safe, so a mismatch would
// hand one caller another's result. Each carries a marker the server echoes,
// and the replies come back in reverse to keep arrival order off the answer.
func TestStdioOverlappingCallsEachGetTheirOwnReply(t *testing.T) {
	const callers = 24
	tr, server, cleanup := stdioPipePair(t, "das")
	defer cleanup()

	served := make(chan error, 1)
	go func() {
		reqs, err := server.readRequests(callers)
		if err != nil {
			served <- err
			return
		}
		for _, v := range slices.Backward(reqs) {
			if err := server.reply(*v.ID, map[string]any{"marker": v.Params.Arguments.Marker}); err != nil {
				served <- err
				return
			}
		}
		served <- nil
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	wrong := make(chan string, callers)
	for i := range callers {
		wg.Go(func() {
			marker := fmt.Sprintf("caller-%d", i)
			raw, err := tr.call(ctx, "tools/call", map[string]any{"arguments": map[string]any{"marker": marker}})
			if err != nil {
				wrong <- fmt.Sprintf("%s: %v", marker, err)
				return
			}
			var got struct {
				Marker string `json:"marker"`
			}
			if err := json.Unmarshal(raw, &got); err != nil {
				wrong <- fmt.Sprintf("%s: decode: %v", marker, err)
				return
			}
			if got.Marker != marker {
				wrong <- fmt.Sprintf("%s received %q", marker, got.Marker)
			}
		})
	}
	wg.Wait()
	close(wrong)

	if err := <-served; err != nil {
		t.Fatalf("server: %v", err)
	}
	for msg := range wrong {
		t.Error(msg)
	}
}

// The second call must answer to its own deadline while the first is still out.
func TestStdioCallDeadlineHoldsWhileAnotherIsInFlight(t *testing.T) {
	tr := &stdioTransport{name: "das", stdin: discardWriteCloser{}, pending: map[int]chan rpcResponse{}}

	stuck := t.Context()
	go func() { _, _ = tr.call(stuck, "tools/call", map[string]any{"name": "run_flow_screening"}) }()
	time.Sleep(150 * time.Millisecond) // let it park waiting for a reply

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	done := make(chan error, 1)
	go func() {
		_, err := tr.call(ctx, "tools/call", map[string]any{"name": "query_model"})
		done <- err
	}()

	select {
	case err := <-done:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("second call returned %v, want its own deadline", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("second call never returned; it is queued behind the first")
	}
}

// An already-cancelled call must come back at once even with another in flight:
// this is the path an abandoned turn takes, and it cannot depend on a stranger.
func TestStdioCancelledCallReturnsWhileAnotherIsInFlight(t *testing.T) {
	tr := &stdioTransport{name: "das", stdin: discardWriteCloser{}, pending: map[int]chan rpcResponse{}}

	stuck := t.Context()
	go func() { _, _ = tr.call(stuck, "tools/call", map[string]any{"name": "run_flow_screening"}) }()
	time.Sleep(150 * time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	done := make(chan error, 1)
	go func() {
		_, err := tr.call(ctx, "tools/call", map[string]any{"name": "list_fields"})
		done <- err
	}()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("pre-cancelled call returned %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("a pre-cancelled call still hangs, so the turn cannot be abandoned")
	}
}

// Walking away is not enough: the server keeps working, and a long query keeps
// whatever it locked. MCP's cancellation notification is how it learns.
func TestStdioCancelNotifiesTheServer(t *testing.T) {
	tr, server, cleanup := stdioPipePair(t, "das")
	defer cleanup()

	ctx, cancel := context.WithCancel(context.Background())
	go func() { _, _ = tr.call(ctx, "tools/call", map[string]any{"name": "run_flow_screening"}) }()

	ids, err := server.readRequestIDs(1)
	if err != nil {
		t.Fatalf("server did not receive the call: %v", err)
	}
	cancel()

	note, err := server.readNotification()
	if err != nil {
		t.Fatalf("server never heard the call was abandoned: %v", err)
	}
	if note.Method != cancelledMethod {
		t.Fatalf("method = %q, want %q", note.Method, cancelledMethod)
	}
	if got := int(note.Params.RequestID); got != ids[0] {
		t.Errorf("requestId = %d, want the abandoned call's id %d", got, ids[0])
	}
	if note.Params.Reason == "" {
		t.Error("reason is empty; the server logs it to explain the abort")
	}
}

// Spec: a client MUST NOT cancel initialize.
func TestCancelInFlightLeavesInitializeAlone(t *testing.T) {
	rec := &notifyRecorder{seen: make(chan string, 2)}
	cancelInFlight(rec, initializeMethod, 1, context.Canceled)
	cancelInFlight(rec, "tools/call", 2, context.Canceled)

	select {
	case method := <-rec.seen:
		if method != cancelledMethod {
			t.Fatalf("first notification = %q", method)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the tools/call cancellation never went out")
	}
	select {
	case method := <-rec.seen:
		t.Fatalf("a second notification went out (%q); initialize must not be cancelled", method)
	case <-time.After(200 * time.Millisecond):
	}
}

type notifyRecorder struct{ seen chan string }

func (n *notifyRecorder) call(context.Context, string, any) (json.RawMessage, error) {
	return nil, errors.New("unused")
}

func (n *notifyRecorder) notify(_ context.Context, method string, _ any) error {
	n.seen <- method
	return nil
}

func (n *notifyRecorder) close() {}

// stdioPipePair wires a transport to a scriptable server over in-memory pipes.
func stdioPipePair(t *testing.T, name string) (*stdioTransport, *scriptedStdioServer, func()) {
	t.Helper()
	serverReads, clientWrites := io.Pipe()
	clientReads, serverWrites := io.Pipe()
	tr := &stdioTransport{
		name:    name,
		stdin:   clientWrites,
		stdout:  bufio.NewReader(clientReads),
		stderr:  &tailBuffer{limit: 1024},
		pending: map[int]chan rpcResponse{},
	}
	go tr.readLoop()
	server := &scriptedStdioServer{
		dec: json.NewDecoder(serverReads),
		enc: json.NewEncoder(serverWrites),
	}
	return tr, server, func() {
		_ = clientWrites.Close()
		_ = serverReads.Close()
		_ = serverWrites.Close()
		_ = clientReads.Close()
	}
}

type scriptedStdioServer struct {
	dec *json.Decoder
	enc *json.Encoder
}

type stdioNotification struct {
	Method string `json:"method"`
	Params struct {
		RequestID float64 `json:"requestId"`
		Reason    string  `json:"reason"`
	} `json:"params"`
}

type stdioRequest struct {
	ID     *int `json:"id"`
	Params struct {
		Arguments struct {
			Marker string `json:"marker"`
		} `json:"arguments"`
	} `json:"params"`
}

// readRequests consumes n id-bearing requests, skipping notifications.
func (s *scriptedStdioServer) readRequests(n int) ([]stdioRequest, error) {
	var reqs []stdioRequest
	for len(reqs) < n {
		var msg stdioRequest
		if err := s.dec.Decode(&msg); err != nil {
			return nil, err
		}
		if msg.ID != nil {
			reqs = append(reqs, msg)
		}
	}
	return reqs, nil
}

func (s *scriptedStdioServer) readRequestIDs(n int) ([]int, error) {
	reqs, err := s.readRequests(n)
	if err != nil {
		return nil, err
	}
	ids := make([]int, len(reqs))
	for i, r := range reqs {
		ids[i] = *r.ID
	}
	return ids, nil
}

func (s *scriptedStdioServer) readNotification() (stdioNotification, error) {
	for {
		var msg stdioNotification
		if err := s.dec.Decode(&msg); err != nil {
			return msg, err
		}
		if msg.Method != "" {
			return msg, nil
		}
	}
}

func (s *scriptedStdioServer) reply(id int, result any) error {
	return s.enc.Encode(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
}
