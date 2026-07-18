package daemonprobe

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"reasonix/internal/remote/protocol"
	"reasonix/internal/rpcwire"
)

const probeTestTimeout = 3 * time.Second

type exactBehavior uint8

const (
	exactSucceeds exactBehavior = iota
	exactBusy
)

type fakeDaemonDialer struct {
	mu       sync.Mutex
	buildAt  func(int) protocol.BuildID
	behavior exactBehavior
	dials    int
	exact    int
	busy     int
	detaches int
	active   int
	wg       sync.WaitGroup
}

func (d *fakeDaemonDialer) Dial(ctx context.Context) (net.Conn, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	d.mu.Lock()
	index := d.dials
	d.dials++
	buildID := d.buildAt(index)
	d.mu.Unlock()
	serverSide, clientSide := net.Pipe()
	d.wg.Add(1)
	go func() {
		defer d.wg.Done()
		d.serve(serverSide, buildID, index)
	}()
	return clientSide, nil
}

func (d *fakeDaemonDialer) serve(raw net.Conn, buildID protocol.BuildID, index int) {
	defer raw.Close()
	var lease protocol.LeaseID
	wire := rpcwire.NewConn(raw, raw, rpcwire.Options{
		Name:             "fake-daemon",
		MaxInboundBytes:  protocol.FrameBytes,
		MaxOutboundBytes: protocol.FrameBytes,
		StrictJSONRPC:    true,
	})
	wire.Handle(string(protocol.MethodRemoteInitialize), func(_ context.Context, rawParams json.RawMessage) (any, error) {
		decoded, err := protocol.DecodeRequestParams(protocol.MethodRemoteInitialize, rawParams)
		if err != nil {
			return nil, &rpcwire.RPCError{Code: rpcwire.ErrInvalidParams, Message: "invalid params"}
		}
		params := decoded.(protocol.InitializeParams)
		if params.ResumeLeaseID != "" {
			return nil, &rpcwire.RPCError{Code: rpcwire.ErrInvalidParams, Message: "probe supplied resume lease"}
		}
		if err := protocol.CompareBuildID(buildID, params.BuildID); err != nil {
			var mismatch *protocol.BuildIDMismatch
			if !errors.As(err, &mismatch) {
				return nil, err
			}
			return nil, protocol.MustRemoteError(protocol.ErrDaemonRestartRequired, protocol.ErrorOptions{
				Expected: mismatch.Expected, Actual: mismatch.Actual,
			}).RPCError()
		}

		d.mu.Lock()
		d.exact++
		if d.behavior == exactBusy {
			d.busy++
			d.mu.Unlock()
			retry := int64(25)
			return nil, protocol.MustRemoteError(protocol.ErrHostBusy, protocol.ErrorOptions{RetryAfterMs: &retry}).RPCError()
		}
		d.active++
		lease = protocol.LeaseID(fmt.Sprintf("lease-probe-%d", index))
		d.mu.Unlock()
		return protocol.InitializeResult{
			BuildID:   buildID,
			HostEpoch: protocol.HostEpoch(fmt.Sprintf("host-probe-%d", index)),
			Lease: protocol.LeaseInfo{
				LeaseID: lease, TTLMillis: protocol.LeaseTTLMillis,
				PingIntervalMs: protocol.LeasePingIntervalMillis,
			},
			Host:         protocol.HostInfo{OS: "linux", Arch: "amd64", ShellKind: "bash", SandboxBackend: "none"},
			Capabilities: protocol.FrozenCapabilities(false, false),
		}, nil
	})
	wire.Handle(string(protocol.MethodRemoteDetach), func(_ context.Context, rawParams json.RawMessage) (any, error) {
		decoded, err := protocol.DecodeRequestParams(protocol.MethodRemoteDetach, rawParams)
		if err != nil {
			return nil, &rpcwire.RPCError{Code: rpcwire.ErrInvalidParams, Message: "invalid params"}
		}
		params := decoded.(protocol.DetachParams)
		if lease == "" || params.LeaseID != lease {
			return nil, protocol.MustRemoteError(protocol.ErrLeaseNotHeld, protocol.ErrorOptions{}).RPCError()
		}
		return rpcwire.RespondThen(protocol.DetachResult{Detached: true}, func(writeErr error) {
			if writeErr != nil {
				return
			}
			d.mu.Lock()
			d.detaches++
			d.active--
			lease = ""
			d.mu.Unlock()
		}), nil
	})
	_ = wire.Serve(context.Background())
}

type fakeDaemonStats struct {
	dials    int
	exact    int
	busy     int
	detaches int
	active   int
}

func (d *fakeDaemonDialer) stats() fakeDaemonStats {
	d.mu.Lock()
	defer d.mu.Unlock()
	return fakeDaemonStats{d.dials, d.exact, d.busy, d.detaches, d.active}
}

func (d *fakeDaemonDialer) wait(t *testing.T) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		d.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(probeTestTimeout):
		t.Fatal("timed out waiting for fake daemon connections")
	}
}

func TestProbeRecoversStableBuildIDWithoutAcquiringLease(t *testing.T) {
	daemonBuild := testBuildID("daemon", 'a', "1", 'b')
	daemon := &fakeDaemonDialer{buildAt: func(int) protocol.BuildID { return daemonBuild }}
	client := deterministicClient(daemon, func(call int) protocol.BuildID {
		return testBuildID(fmt.Sprintf("candidate-%d", call), byte('1'+call%8), fmt.Sprintf("probe-%d", call), byte('8'-call%8))
	})

	result := probeResult(t, client)
	if err := protocol.CompareBuildID(daemonBuild, result); err != nil {
		t.Fatalf("probe Build ID mismatch: %v\n got: %+v\nwant: %+v", err, result, daemonBuild)
	}
	daemon.wait(t)
	stats := daemon.stats()
	if stats.dials != 8 || stats.exact != 0 || stats.busy != 0 || stats.detaches != 0 || stats.active != 0 {
		t.Fatalf("normal probe touched lease state: %+v", stats)
	}
}

func TestProbeUsesHostBusyAsExactIdentityWithoutLease(t *testing.T) {
	daemonBuild := testBuildID("daemon-busy", 'c', "1", 'd')
	daemon := &fakeDaemonDialer{buildAt: func(int) protocol.BuildID { return daemonBuild }, behavior: exactBusy}
	client := deterministicClient(daemon, func(int) protocol.BuildID { return daemonBuild })

	result := probeResult(t, client)
	if err := protocol.CompareBuildID(daemonBuild, result); err != nil {
		t.Fatalf("HOST_BUSY identity mismatch: %v", err)
	}
	daemon.wait(t)
	stats := daemon.stats()
	if stats.dials != 2 || stats.exact != 2 || stats.busy != 2 || stats.detaches != 0 || stats.active != 0 {
		t.Fatalf("HOST_BUSY probe lease state: %+v", stats)
	}
}

func TestProbeDetachesExtremelyExactInitializeCandidate(t *testing.T) {
	daemonBuild := testBuildID("daemon-exact", 'e', "1", 'f')
	daemon := &fakeDaemonDialer{buildAt: func(int) protocol.BuildID { return daemonBuild }}
	client := deterministicClient(daemon, func(int) protocol.BuildID { return daemonBuild })

	result := probeResult(t, client)
	if err := protocol.CompareBuildID(daemonBuild, result); err != nil {
		t.Fatalf("exact identity mismatch: %v", err)
	}
	daemon.wait(t)
	stats := daemon.stats()
	if stats.dials != 2 || stats.exact != 2 || stats.detaches != 2 || stats.active != 0 {
		t.Fatalf("exact probe retained a lease: %+v", stats)
	}
}

func TestProbeDiscardsSeriesWhenDaemonRestartsBetweenConnections(t *testing.T) {
	first := testBuildID("daemon-old", '1', "1", '2')
	second := testBuildID("daemon-new", '3', "1", '4')
	daemon := &fakeDaemonDialer{buildAt: func(connection int) protocol.BuildID {
		if connection == 0 {
			return first
		}
		return second
	}}
	client := deterministicClient(daemon, deterministicCandidate)

	result := probeResult(t, client)
	if err := protocol.CompareBuildID(second, result); err != nil {
		t.Fatalf("restarted daemon identity mismatch: %v\n got: %+v\nwant: %+v", err, result, second)
	}
	daemon.wait(t)
	stats := daemon.stats()
	if stats.dials != 10 || stats.exact != 0 || stats.active != 0 {
		t.Fatalf("restart recovery stats = %+v, want 10 mismatch-only connections", stats)
	}
}

func TestProbeRejectsContinuouslyChangingDaemon(t *testing.T) {
	first := testBuildID("daemon-a", '5', "1", '6')
	second := testBuildID("daemon-b", '7', "1", '8')
	daemon := &fakeDaemonDialer{buildAt: func(connection int) protocol.BuildID {
		if connection%2 == 0 {
			return first
		}
		return second
	}}
	client := deterministicClient(daemon, deterministicCandidate)
	client.maxSeries = 4

	ctx, cancel := context.WithTimeout(context.Background(), probeTestTimeout)
	defer cancel()
	_, err := client.Probe(ctx)
	if !errors.Is(err, ErrDaemonUnstable) {
		t.Fatalf("Probe error = %v, want ErrDaemonUnstable", err)
	}
	daemon.wait(t)
	if stats := daemon.stats(); stats.dials != 8 || stats.exact != 0 || stats.active != 0 {
		t.Fatalf("unstable daemon stats = %+v", stats)
	}
}

func TestProbeRejectsMalformedInitializeErrors(t *testing.T) {
	candidate := deterministicCandidate(0)
	retry := int64(10)
	validBusy := protocol.MustRemoteError(protocol.ErrHostBusy, protocol.ErrorOptions{RetryAfterMs: &retry})
	validMismatch := protocol.MustRemoteError(protocol.ErrDaemonRestartRequired, protocol.ErrorOptions{
		Expected: "daemon", Actual: candidate.ProductVersion,
	})
	tests := []struct {
		name string
		err  *rpcwire.RPCError
	}{
		{name: "non domain code", err: &rpcwire.RPCError{Code: rpcwire.ErrInternal, Message: "internal error"}},
		{name: "unknown data member", err: &rpcwire.RPCError{Code: protocol.DomainErrorCode, Message: validMismatch.Message, Data: map[string]any{
			"reasonixCode": protocol.ErrDaemonRestartRequired, "retryable": false, "action": protocol.ActionRestartDaemon,
			"suggestedCommand": "reasonix remote restart", "expected": "daemon", "actual": candidate.ProductVersion, "extra": true,
		}}},
		{name: "wrong frozen message", err: &rpcwire.RPCError{Code: protocol.DomainErrorCode, Message: "wrong", Data: validBusy.Data}},
		{name: "unexpected actual", err: protocol.MustRemoteError(protocol.ErrDaemonRestartRequired, protocol.ErrorOptions{
			Expected: "daemon", Actual: "not-a-candidate-field",
		}).RPCError()},
		{name: "busy with mismatch fields", err: &rpcwire.RPCError{Code: protocol.DomainErrorCode, Message: validBusy.Message, Data: protocol.RemoteErrorData{
			ReasonixCode: protocol.ErrHostBusy, Retryable: true, Action: protocol.ActionRetry,
			RetryAfterMs: &retry, Expected: "daemon", Actual: candidate.ProductVersion,
		}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dialer := handlerDialer(func(context.Context, json.RawMessage) (any, error) { return nil, test.err })
			client := deterministicClient(dialer, func(int) protocol.BuildID { return candidate })
			ctx, cancel := context.WithTimeout(context.Background(), probeTestTimeout)
			defer cancel()
			if _, err := client.Probe(ctx); err == nil {
				t.Fatal("Probe accepted malformed initialize error")
			}
		})
	}
}

func TestProbeTimeoutCancellationAndEndpointFailures(t *testing.T) {
	t.Run("stalled response has bounded attempt", func(t *testing.T) {
		dialer := endpointDialerFunc(func(context.Context) (net.Conn, error) {
			serverSide, clientSide := net.Pipe()
			go func() {
				defer serverSide.Close()
				_, _ = bufio.NewReader(serverSide).ReadBytes('\n')
				_, _ = bufio.NewReader(serverSide).ReadByte()
			}()
			return clientSide, nil
		})
		client := deterministicClient(dialer, deterministicCandidate)
		client.attemptTimeout = 25 * time.Millisecond
		started := time.Now()
		_, err := client.Probe(context.Background())
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("Probe error = %v, want deadline exceeded", err)
		}
		if elapsed := time.Since(started); elapsed > time.Second {
			t.Fatalf("stalled probe took %s", elapsed)
		}
	})

	t.Run("cancelled before dial", func(t *testing.T) {
		dials := 0
		dialer := endpointDialerFunc(func(context.Context) (net.Conn, error) {
			dials++
			return nil, errors.New("unexpected")
		})
		client := deterministicClient(dialer, deterministicCandidate)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		if _, err := client.Probe(ctx); !errors.Is(err, context.Canceled) {
			t.Fatalf("Probe error = %v, want context canceled", err)
		}
		if dials != 0 {
			t.Fatalf("cancelled Probe dialed %d times", dials)
		}
	})

	t.Run("dial error", func(t *testing.T) {
		failure := errors.New("socket unavailable")
		client := deterministicClient(endpointDialerFunc(func(context.Context) (net.Conn, error) {
			return nil, failure
		}), deterministicCandidate)
		ctx, cancel := context.WithTimeout(context.Background(), probeTestTimeout)
		defer cancel()
		_, err := client.Probe(ctx)
		if !errors.Is(err, failure) {
			t.Fatalf("Probe error = %v, want socket failure", err)
		}
	})

	t.Run("nil connection", func(t *testing.T) {
		client := deterministicClient(endpointDialerFunc(func(context.Context) (net.Conn, error) {
			return nil, nil
		}), deterministicCandidate)
		ctx, cancel := context.WithTimeout(context.Background(), probeTestTimeout)
		defer cancel()
		if _, err := client.Probe(ctx); err == nil || !strings.Contains(err.Error(), "nil connection") {
			t.Fatalf("Probe error = %v, want nil connection rejection", err)
		}
	})
}

func TestProbeRejectsMalformedStrictResponseFrame(t *testing.T) {
	dialer := endpointDialerFunc(func(context.Context) (net.Conn, error) {
		serverSide, clientSide := net.Pipe()
		go func() {
			defer serverSide.Close()
			_, _ = bufio.NewReader(serverSide).ReadBytes('\n')
			_, _ = serverSide.Write([]byte(`{"jsonrpc":"2.0","id":1,"error":{"code":"bad","message":"bad"}}` + "\n"))
		}()
		return clientSide, nil
	})
	client := deterministicClient(dialer, deterministicCandidate)
	ctx, cancel := context.WithTimeout(context.Background(), probeTestTimeout)
	defer cancel()
	if _, err := client.Probe(ctx); err == nil {
		t.Fatal("Probe accepted malformed strict JSON-RPC response")
	}
}

type endpointDialerFunc func(context.Context) (net.Conn, error)

func (f endpointDialerFunc) Dial(ctx context.Context) (net.Conn, error) { return f(ctx) }

func handlerDialer(handler rpcwire.RequestHandler) endpointDialer {
	return endpointDialerFunc(func(context.Context) (net.Conn, error) {
		serverSide, clientSide := net.Pipe()
		go func() {
			defer serverSide.Close()
			wire := rpcwire.NewConn(serverSide, serverSide, rpcwire.Options{
				Name: "probe-error-server", MaxInboundBytes: protocol.FrameBytes,
				MaxOutboundBytes: protocol.FrameBytes, StrictJSONRPC: true,
			})
			wire.Handle(string(protocol.MethodRemoteInitialize), handler)
			_ = wire.Serve(context.Background())
		}()
		return clientSide, nil
	})
}

func deterministicClient(dialer endpointDialer, candidate func(int) protocol.BuildID) *Client {
	client := newClient(dialer)
	call := 0
	client.newCandidate = func() (protocol.BuildID, error) {
		value := candidate(call)
		call++
		return value, nil
	}
	clientID := 0
	client.newClientID = func() (protocol.ClientInstanceID, error) {
		clientID++
		return protocol.ClientInstanceID(fmt.Sprintf("probe-client-%d", clientID)), nil
	}
	return client
}

func deterministicCandidate(call int) protocol.BuildID {
	return testBuildID(fmt.Sprintf("probe-product-%d", call), byte('a'+call%6), fmt.Sprintf("probe-protocol-%d", call), byte('0'+call%10))
}

func testBuildID(product string, revisionByte byte, protocolVersion string, schemaByte byte) protocol.BuildID {
	return protocol.BuildID{
		ProductVersion: product, SourceRevision: strings.Repeat(string(revisionByte), 40),
		ProtocolVersion: protocolVersion, SchemaHash: "sha256:" + strings.Repeat(string(schemaByte), 64),
	}
}

func probeResult(t *testing.T, client *Client) protocol.BuildID {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), probeTestTimeout)
	defer cancel()
	result, err := client.Probe(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return result.BuildID
}
