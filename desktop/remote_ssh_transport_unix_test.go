//go:build !windows

package main

import (
	"bufio"
	"context"
	"os/exec"
	"testing"
	"time"
)

func TestRemoteSSHTransportContextCancellationStopsProcessGroup(t *testing.T) {
	t.Setenv("GO_WANT_REMOTE_SSH_FAKE", "1")
	t.Setenv("REMOTE_SSH_FAKE_MODE", "hang")
	ctx, cancel := context.WithCancel(context.Background())
	factory := &RemoteSSHTransportFactory{commandContext: func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
		return remoteSSHFakeCommand(ctx)
	}}
	transport, err := factory.Start(ctx, "host")
	if err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(transport).ReadString('\n')
	if err != nil || line != "ready\n" {
		t.Fatalf("read readiness = %q, %v", line, err)
	}
	cancel()
	done := make(chan error, 1)
	go func() { done <- transport.Wait() }()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("Unix SSH process group survived context cancellation")
	}
}
