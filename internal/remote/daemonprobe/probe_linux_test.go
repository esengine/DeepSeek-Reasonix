//go:build linux

package daemonprobe

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/remote/catalog"
	"reasonix/internal/remote/daemon"
	"reasonix/internal/remote/protocol"
	"reasonix/internal/remote/service"
	"reasonix/internal/rpcwire"
)

type unusedControllerFactory struct{}

func (unusedControllerFactory) CreateController(context.Context, protocol.RuntimeTarget, event.Sink) (control.SessionAPI, error) {
	return nil, errors.New("daemon probe integration unexpectedly constructed a Session controller")
}

func TestProbeThroughSecureUnixEndpointAndRealDaemonLeavesLeaseFree(t *testing.T) {
	buildID := testBuildID("daemon-linux-integration", '9', protocol.ProtocolVersion, 'a')
	hostEpoch := protocol.HostEpoch("host-linux-probe")
	// Linux sockaddr_un paths are commonly limited to 108 bytes. Go's
	// descriptive t.TempDir path can exceed that before /reasonix/remote.sock is
	// appended, so keep this real socket integration root deliberately short.
	stateRoot, err := os.MkdirTemp("/tmp", "rdp-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(stateRoot) })
	catalogValue, err := catalog.New(hostEpoch, catalog.Options{
		StateDir: filepath.Join(stateRoot, "catalog"),
		UserHome: stateRoot,
	})
	if err != nil {
		t.Fatal(err)
	}
	server, err := daemon.New(context.Background(), daemon.Options{
		BuildID:           buildID,
		HostEpoch:         hostEpoch,
		HostInfo:          protocol.HostInfo{OS: "linux", Arch: "amd64", ShellKind: "bash", SandboxBackend: "none"},
		Capabilities:      protocol.FrozenCapabilities(false, false),
		Catalog:           catalogValue,
		ControllerFactory: unusedControllerFactory{},
		Metadata: func(context.Context, protocol.RuntimeTarget) (protocol.SessionMetaSnapshot, error) {
			return protocol.SessionMetaSnapshot{}, errors.New("unexpected metadata read")
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	runtimeDir := filepath.Join(stateRoot, "runtime")
	if err := os.Mkdir(runtimeDir, 0o700); err != nil {
		server.Close()
		t.Fatal(err)
	}
	endpoint := service.NewEndpoint(service.Paths{
		RuntimeDir: runtimeDir,
		UnitPath:   filepath.Join(stateRoot, service.UnitName),
	})
	listener, err := endpoint.Listen()
	if err != nil {
		server.Close()
		t.Fatal(err)
	}
	serverDone := make(chan error, 1)
	go func() { serverDone <- server.ServeListener(listener) }()
	t.Cleanup(func() {
		server.Close()
		select {
		case serveErr := <-serverDone:
			if serveErr != nil {
				t.Errorf("daemon ServeListener: %v", serveErr)
			}
		case <-time.After(probeTestTimeout):
			t.Error("timed out stopping integration daemon")
		}
	})

	probe, err := New(endpoint)
	if err != nil {
		t.Fatal(err)
	}
	result := probeResult(t, probe)
	if err := protocol.CompareBuildID(buildID, result); err != nil {
		t.Fatalf("real daemon probe Build ID mismatch: %v", err)
	}

	// A fresh exact initialize must succeed immediately. If the probe had kept
	// even one temporary lease, the real daemon would return HOST_BUSY here.
	connection, err := endpoint.Dial(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	clientCtx, cancelClient := context.WithCancel(context.Background())
	wire := rpcwire.NewConn(connection, connection, rpcwire.Options{
		Name: "daemon-probe-lease-check", MaxInboundBytes: protocol.FrameBytes,
		MaxOutboundBytes: protocol.FrameBytes, StrictJSONRPC: true,
	})
	clientDone := make(chan error, 1)
	go func() { clientDone <- wire.Serve(clientCtx) }()
	defer func() {
		cancelClient()
		_ = connection.Close()
		select {
		case <-clientDone:
		case <-time.After(probeTestTimeout):
			t.Error("timed out stopping lease-check client")
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), probeTestTimeout)
	defer cancel()
	rawInitialize, err := wire.Request(ctx, string(protocol.MethodRemoteInitialize), protocol.InitializeParams{
		BuildID: buildID, ClientInstanceID: "post-probe-client",
	})
	if err != nil {
		t.Fatalf("exact initialize after probe: %v", err)
	}
	var initialized protocol.InitializeResult
	if err := json.Unmarshal(rawInitialize, &initialized); err != nil {
		t.Fatal(err)
	}
	rawDetach, err := wire.Request(ctx, string(protocol.MethodRemoteDetach), protocol.DetachParams{LeaseID: initialized.Lease.LeaseID})
	if err != nil {
		t.Fatalf("detach lease-check client: %v", err)
	}
	var detached protocol.DetachResult
	if err := json.Unmarshal(rawDetach, &detached); err != nil || !detached.Detached {
		t.Fatalf("detach result = %+v, %v", detached, err)
	}
}

var _ interface {
	ServeListener(net.Listener) error
} = (*daemon.Server)(nil)
