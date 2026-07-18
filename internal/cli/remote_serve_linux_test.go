//go:build linux

package cli

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"reasonix/internal/remote/protocol"
	"reasonix/internal/remote/service"
	"reasonix/internal/rpcwire"
)

func TestProductionRemoteServeBindsUserSocketAndCompletesInitialize(t *testing.T) {
	root, err := os.MkdirTemp("/tmp", "rx-remote-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	runtimeDir := filepath.Join(root, "runtime")
	if err := os.Mkdir(runtimeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", filepath.Join(root, "home"))
	t.Setenv("REASONIX_HOME", filepath.Join(root, "reasonix-home"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(root, "config"))
	t.Setenv("XDG_RUNTIME_DIR", runtimeDir)

	buildID := hostappTestCLIID(t)
	hostCtx, stopHost := context.WithCancel(context.Background())
	hostDone := make(chan error, 1)
	go func() { hostDone <- runProductionRemoteServe(hostCtx, buildID, io.Discard) }()

	endpoint, err := service.DefaultEndpoint()
	if err != nil {
		t.Fatal(err)
	}
	var connection net.Conn
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		attemptCtx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		connection, err = endpoint.Dial(attemptCtx)
		cancel()
		if err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil {
		stopHost()
		hostErr := <-hostDone
		t.Fatalf("dial production Remote socket: %v (Host: %v)", err, hostErr)
	}

	client := rpcwire.NewConn(connection, connection, rpcwire.Options{
		Name: "remote-serve-cli-test", MaxInboundBytes: protocol.FrameBytes,
		MaxOutboundBytes: protocol.FrameBytes, StrictJSONRPC: true,
	})
	clientCtx, stopClient := context.WithCancel(context.Background())
	clientDone := make(chan error, 1)
	go func() { clientDone <- client.Serve(clientCtx) }()
	requestCtx, cancelRequest := context.WithTimeout(context.Background(), 3*time.Second)
	raw, err := client.Request(requestCtx, string(protocol.MethodRemoteInitialize), protocol.InitializeParams{
		BuildID: buildID, ClientInstanceID: "desktop-production-test",
	})
	cancelRequest()
	if err != nil {
		t.Fatal(err)
	}
	var initialized protocol.InitializeResult
	if err := json.Unmarshal(raw, &initialized); err != nil {
		t.Fatal(err)
	}
	if initialized.BuildID != buildID || initialized.HostEpoch == "" || initialized.Lease.LeaseID == "" ||
		initialized.Lease.TTLMillis != protocol.LeaseTTLMillis || initialized.Lease.PingIntervalMs != protocol.LeasePingIntervalMillis {
		t.Fatalf("initialize = %+v", initialized)
	}

	stopClient()
	_ = connection.Close()
	select {
	case <-clientDone:
	case <-time.After(3 * time.Second):
		t.Fatal("Remote client did not stop")
	}
	stopHost()
	select {
	case err := <-hostDone:
		if err != nil {
			t.Fatalf("production Remote Host stopped with error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("production Remote Host did not stop")
	}
}

func hostappTestCLIID(t *testing.T) protocol.BuildID {
	t.Helper()
	id, err := protocol.NewBuildID("v-remote-serve-test", remoteCLIRevision)
	if err != nil {
		t.Fatal(err)
	}
	return id
}
