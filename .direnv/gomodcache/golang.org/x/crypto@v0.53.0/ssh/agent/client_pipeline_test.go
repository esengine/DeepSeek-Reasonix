// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package agent

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestPipelineParallelSigns(t *testing.T) {
	c1, c2, err := netPipe()
	if err != nil {
		t.Fatalf("netPipe: %v", err)
	}
	defer c1.Close()
	defer c2.Close()

	keyring := NewKeyring()
	if err := keyring.Add(AddedKey{PrivateKey: testPrivateKeys["rsa"]}); err != nil {
		t.Fatal(err)
	}
	go ServeAgent(keyring, c2)

	client := NewClient(c1)
	pubKey := testPublicKeys["rsa"]

	const N = 64
	var wg sync.WaitGroup
	errCh := make(chan error, N)
	for i := range N {
		wg.Go(func() {
			data := []byte{byte(i), byte(i >> 8)}
			sig, err := client.Sign(pubKey, data)
			if err != nil {
				errCh <- err
				return
			}
			if err := pubKey.Verify(data, sig); err != nil {
				errCh <- err
			}
		})
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Error(err)
	}
}

func TestPipelineBackpressure(t *testing.T) {
	c1, c2, err := netPipe()
	if err != nil {
		t.Fatalf("netPipe: %v", err)
	}

	const total = pipelineMaxInFlight + 8

	// Fake agent: the reader drains requests from the wire independently
	// of response production, so the write path is never self-blocked.
	// The responder only writes a canned success reply once unblocked
	// via gate.
	var reqsRead int64
	gate := make(chan struct{}, total)
	pending := make(chan struct{}, total+pipelineMaxInFlight)
	readerDone := make(chan struct{})
	responderDone := make(chan struct{})
	go func() {
		defer close(readerDone)
		defer close(pending)
		for {
			var sizeBuf [4]byte
			if _, err := io.ReadFull(c2, sizeBuf[:]); err != nil {
				return
			}
			size := binary.BigEndian.Uint32(sizeBuf[:])
			req := make([]byte, size)
			if _, err := io.ReadFull(c2, req); err != nil {
				return
			}
			atomic.AddInt64(&reqsRead, 1)
			pending <- struct{}{}
		}
	}()
	go func() {
		defer close(responderDone)
		for range pending {
			<-gate
			var out [5]byte
			binary.BigEndian.PutUint32(out[:4], 1)
			out[4] = agentSuccess
			if _, err := c2.Write(out[:]); err != nil {
				return
			}
		}
	}()
	defer func() {
		c1.Close()
		c2.Close()
		<-readerDone
		<-responderDone
	}()

	client := NewClient(c1)

	var wg sync.WaitGroup
	errs := make(chan error, total)
	for range total {
		wg.Go(func() {
			// RemoveAll maps to a simpleCall expecting agentSuccess.
			if err := client.RemoveAll(); err != nil {
				errs <- err
			}
		})
	}

	// Wait for the client to fill the pipeline.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if atomic.LoadInt64(&reqsRead) >= pipelineMaxInFlight {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if got := atomic.LoadInt64(&reqsRead); got != pipelineMaxInFlight {
		t.Errorf("requests in flight before any response: got %d, want %d", got, pipelineMaxInFlight)
	}

	// Release all responses; every caller should eventually complete.
	for range total {
		gate <- struct{}{}
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}

	if got := atomic.LoadInt64(&reqsRead); got != total {
		t.Errorf("total requests received: got %d, want %d", got, total)
	}
}

func TestPipelineConnCloseFailsInFlight(t *testing.T) {
	c1, c2, err := netPipe()
	if err != nil {
		t.Fatalf("netPipe: %v", err)
	}

	// Server that reads requests but never replies, so all calls are
	// parked waiting for responses.
	serverExit := make(chan struct{})
	go func() {
		defer close(serverExit)
		buf := make([]byte, 4096)
		for {
			if _, err := c2.Read(buf); err != nil {
				return
			}
		}
	}()

	client := NewClient(c1)

	const N = 8
	var wg sync.WaitGroup
	errs := make(chan error, N)
	for range N {
		wg.Go(func() {
			errs <- client.RemoveAll()
		})
	}

	// Give callers time to enter the pipeline, then tear down the conn.
	time.Sleep(50 * time.Millisecond)
	c1.Close()
	c2.Close()

	// All callers must return promptly with an error.
	doneCh := make(chan struct{})
	go func() {
		wg.Wait()
		close(doneCh)
	}()
	select {
	case <-doneCh:
	case <-time.After(5 * time.Second):
		t.Fatal("pipelined callers did not complete after conn close")
	}

	close(errs)
	for err := range errs {
		if err == nil {
			t.Error("expected error after conn close, got nil")
		}
	}

	// Subsequent calls also fail fast rather than hanging.
	errCh := make(chan error, 1)
	go func() { errCh <- client.RemoveAll() }()
	select {
	case err := <-errCh:
		if err == nil {
			t.Error("expected error on post-close call, got nil")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("post-close call hung")
	}

	<-serverExit
}

// readWriter exposes only io.Reader and io.Writer, hiding any Close
// method on the wrapped value so NewClient cannot take the pipelined
// path.
type readWriter struct {
	io.Reader
	io.Writer
}

func TestNewClientSerialFallback(t *testing.T) {
	c1, c2, err := netPipe()
	if err != nil {
		t.Fatalf("netPipe: %v", err)
	}
	defer c1.Close()
	defer c2.Close()

	keyring := NewKeyring()
	if err := keyring.Add(AddedKey{PrivateKey: testPrivateKeys["rsa"]}); err != nil {
		t.Fatal(err)
	}
	go ServeAgent(keyring, c2)

	rw := readWriter{Reader: c1, Writer: c1}
	ag := NewClient(rw)

	ci, ok := ag.(*client)
	if !ok {
		t.Fatalf("NewClient: got %T, want *client", ag)
	}
	if ci.pipeline != nil {
		t.Error("NewClient with non-Closer rw: pipeline must be nil")
	}
	if ci.conn == nil {
		t.Error("NewClient with non-Closer rw: conn must be set")
	}

	// The serialized path correctly handles a request/response cycle.
	keys, err := ag.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(keys) != 1 {
		t.Errorf("List: got %d keys, want 1", len(keys))
	}
}

func TestSerialFallbackParallelSigns(t *testing.T) {
	c1, c2, err := netPipe()
	if err != nil {
		t.Fatalf("netPipe: %v", err)
	}
	defer c1.Close()
	defer c2.Close()

	keyring := NewKeyring()
	if err := keyring.Add(AddedKey{PrivateKey: testPrivateKeys["rsa"]}); err != nil {
		t.Fatal(err)
	}
	go ServeAgent(keyring, c2)

	rw := readWriter{Reader: c1, Writer: c1}
	ag := NewClient(rw)

	if ci, ok := ag.(*client); !ok {
		t.Fatalf("NewClient: got %T, want *client", ag)
	} else if ci.pipeline != nil {
		t.Fatal("pipeline must be nil for non-Closer rw")
	}

	pubKey := testPublicKeys["rsa"]

	const N = 64
	var wg sync.WaitGroup
	errCh := make(chan error, N)
	for i := range N {
		wg.Go(func() {
			data := []byte{byte(i), byte(i >> 8)}
			sig, err := ag.Sign(pubKey, data)
			if err != nil {
				errCh <- err
				return
			}
			if err := pubKey.Verify(data, sig); err != nil {
				errCh <- err
			}
		})
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Error(err)
	}
}

// writeFailConn wraps a net.Conn and forces Write to return an error when
// the fail flag is set. Close delegates to the underlying conn so the test
// can observe whether the pipeline closed it.
type writeFailConn struct {
	net.Conn
	fail atomic.Bool
}

func (c *writeFailConn) Write(p []byte) (int, error) {
	if c.fail.Load() {
		return 0, errors.New("synthetic write failure")
	}
	return c.Conn.Write(p)
}

func TestPipelineWriteErrorClosesConn(t *testing.T) {
	c1, c2, err := netPipe()
	if err != nil {
		t.Fatalf("netPipe: %v", err)
	}
	defer c2.Close()

	// Peer that reads and discards, exiting when the conn closes.
	serverExit := make(chan struct{})
	go func() {
		defer close(serverExit)
		io.Copy(io.Discard, c2)
	}()

	wrapper := &writeFailConn{Conn: c1}
	wrapper.fail.Store(true)

	client := NewClient(wrapper)

	errCh := make(chan error, 1)
	go func() { errCh <- client.RemoveAll() }()

	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("expected error on forced write failure, got nil")
		}
		// The caller whose Write failed must receive the actual
		// write error, not the reader's "closed connection" error
		// from the post-shutdown drain.
		if !strings.Contains(err.Error(), "synthetic write failure") {
			t.Errorf("got %q, want error containing %q", err, "synthetic write failure")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("RemoveAll hung after write failure")
	}

	// The pipeline must have closed the underlying conn, so the peer
	// goroutine returns from its Read.
	select {
	case <-serverExit:
	case <-time.After(2 * time.Second):
		t.Fatal("connection was not closed after write failure")
	}

	// After shutdown, subsequent calls must also fail promptly.
	errCh2 := make(chan error, 1)
	go func() { errCh2 <- client.RemoveAll() }()
	select {
	case err := <-errCh2:
		if err == nil {
			t.Error("expected error on post-shutdown call, got nil")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("post-shutdown call hung")
	}
}

func TestPipelineResponseTooLarge(t *testing.T) {
	c1, c2, err := netPipe()
	if err != nil {
		t.Fatalf("netPipe: %v", err)
	}
	defer c1.Close()
	defer c2.Close()

	// Server: read the request, then reply with a size header that
	// exceeds maxAgentResponseBytes. The size check fires before the
	// body is read, so no body needs to follow.
	serverDone := make(chan struct{})
	go func() {
		defer close(serverDone)
		var sizeBuf [4]byte
		if _, err := io.ReadFull(c2, sizeBuf[:]); err != nil {
			return
		}
		size := binary.BigEndian.Uint32(sizeBuf[:])
		if _, err := io.CopyN(io.Discard, c2, int64(size)); err != nil {
			return
		}
		var out [4]byte
		binary.BigEndian.PutUint32(out[:], maxAgentResponseBytes+1)
		c2.Write(out[:])
	}()

	client := NewClient(c1)

	errCh := make(chan error, 1)
	go func() { errCh <- client.RemoveAll() }()
	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("expected error, got nil")
		}
		if !strings.Contains(err.Error(), "response too large") {
			t.Errorf("got %q, want error containing %q", err, "response too large")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("call hung")
	}

	// Subsequent calls must also fail: the pipeline is shut down.
	errCh2 := make(chan error, 1)
	go func() { errCh2 <- client.RemoveAll() }()
	select {
	case err := <-errCh2:
		if err == nil {
			t.Error("expected error on post-shutdown call, got nil")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("post-shutdown call hung")
	}

	<-serverDone
}

func TestPipelineShutdownWithBlockedWriter(t *testing.T) {
	c1, c2, err := netPipe()
	if err != nil {
		t.Fatalf("netPipe: %v", err)
	}
	defer c1.Close()

	// Server drains requests off the wire so writes never block on
	// backpressure from the transport, but never replies. The reader
	// goroutine therefore stays parked on Read, never dequeues from
	// pending, and the queue fills up.
	var reqsRead int64
	serverExit := make(chan struct{})
	go func() {
		defer close(serverExit)
		for {
			var sizeBuf [4]byte
			if _, err := io.ReadFull(c2, sizeBuf[:]); err != nil {
				return
			}
			size := binary.BigEndian.Uint32(sizeBuf[:])
			if _, err := io.CopyN(io.Discard, c2, int64(size)); err != nil {
				return
			}
			atomic.AddInt64(&reqsRead, 1)
		}
	}()

	client := NewClient(c1)

	const total = pipelineMaxInFlight + 4
	var wg sync.WaitGroup
	errs := make(chan error, total)
	for range total {
		wg.Go(func() {
			errs <- client.RemoveAll()
		})
	}

	// Wait for the queue to fill: once pipelineMaxInFlight requests
	// are on the wire, pending is at capacity and the +1 writer is
	// blocked in the second-select arm; the rest are queued behind
	// writeMu.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if atomic.LoadInt64(&reqsRead) >= pipelineMaxInFlight {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if got := atomic.LoadInt64(&reqsRead); got < pipelineMaxInFlight {
		t.Fatalf("requests on wire: got %d, want >= %d", got, pipelineMaxInFlight)
	}
	// Give the +1 writer a moment to actually park in the second
	// select before we trigger shutdown.
	time.Sleep(50 * time.Millisecond)

	// Trigger shutdown: closing the server side makes the client's
	// reader hit EOF, which calls shutdown(...).
	c2.Close()

	doneCh := make(chan struct{})
	go func() {
		wg.Wait()
		close(doneCh)
	}()
	select {
	case <-doneCh:
	case <-time.After(5 * time.Second):
		t.Fatal("blocked callers did not complete after shutdown")
	}

	close(errs)
	for err := range errs {
		if err == nil {
			t.Error("expected error after shutdown, got nil")
		}
	}

	<-serverExit
}

// multiDeviceAgent simulates an ssh-agent backed by numDevices signing
// devices, each of which takes latency to process a request. Incoming
// requests are round-robin dispatched to devices, and responses are
// serialized back to the wire in request order as the protocol requires.
//
// The simulation does not emit a semantically meaningful reply; it just
// writes a 1-byte SSH_AGENT_SUCCESS after the simulated latency has
// elapsed. This is enough to exercise callRaw/simpleCall (RemoveAll).
func multiDeviceAgent(t testing.TB, conn io.ReadWriteCloser, numDevices int, latency time.Duration) (done <-chan struct{}) {
	t.Helper()

	type job struct {
		ready chan struct{}
	}

	devices := make([]chan job, numDevices)
	var devicesWG sync.WaitGroup
	for i := range numDevices {
		devices[i] = make(chan job, 1024)
		ch := devices[i]
		devicesWG.Go(func() {
			for j := range ch {
				time.Sleep(latency)
				close(j.ready)
			}
		})
	}

	// Order-preserving responder: responses are written in the same
	// order the requests were read, regardless of which device finished
	// first.
	orderCh := make(chan chan struct{}, 1024)
	responderDone := make(chan struct{})
	go func() {
		defer close(responderDone)
		for ready := range orderCh {
			<-ready
			var out [5]byte
			binary.BigEndian.PutUint32(out[:4], 1)
			out[4] = agentSuccess
			if _, err := conn.Write(out[:]); err != nil {
				return
			}
		}
	}()

	finished := make(chan struct{})
	go func() {
		defer close(finished)
		defer func() {
			close(orderCh)
			<-responderDone
			for _, d := range devices {
				close(d)
			}
			devicesWG.Wait()
		}()
		next := 0
		for {
			var sizeBuf [4]byte
			if _, err := io.ReadFull(conn, sizeBuf[:]); err != nil {
				return
			}
			size := binary.BigEndian.Uint32(sizeBuf[:])
			if _, err := io.CopyN(io.Discard, conn, int64(size)); err != nil {
				return
			}
			ready := make(chan struct{})
			devices[next%numDevices] <- job{ready: ready}
			next++
			orderCh <- ready
		}
	}()

	return finished
}

// BenchmarkPipelineMultiDevice measures throughput of the pipelined
// agent client against a simulated multi-device agent, varying the
// number of backend devices.
//
// Setup: each simulated signing device takes 2ms per request. The
// protocol serializes responses in request order, but pipelining lets
// the client keep multiple devices busy concurrently; throughput
// should scale roughly linearly with numDevices up to the pipeline
// depth (pipelineMaxInFlight).
//
// Run with: go test -benchmem -bench BenchmarkPipelineMultiDevice -benchtime=2s ./ssh/agent/
func BenchmarkPipelineMultiDevice(b *testing.B) {
	const (
		deviceLatency = 2 * time.Millisecond
		parallelism   = 32
	)

	for _, numDevices := range []int{1, 2, 4, 8, 16} {
		b.Run(fmt.Sprintf("Devices=%d", numDevices), func(b *testing.B) {
			c1, c2, err := netPipe()
			if err != nil {
				b.Fatalf("netPipe: %v", err)
			}
			serverDone := multiDeviceAgent(b, c2, numDevices, deviceLatency)

			client := NewClient(c1)

			b.ResetTimer()
			b.SetParallelism(parallelism)
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					if err := client.RemoveAll(); err != nil {
						b.Fatal(err)
					}
				}
			})
			b.StopTimer()

			c1.Close()
			c2.Close()
			<-serverDone
		})
	}
}
