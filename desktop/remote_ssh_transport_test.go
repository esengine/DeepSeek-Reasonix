package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestRemoteSSHTransportExactArgvAndProtocolPurity(t *testing.T) {
	t.Setenv("GO_WANT_REMOTE_SSH_FAKE", "1")
	t.Setenv("REMOTE_SSH_FAKE_MODE", "protocol")
	var gotPath string
	var gotArgs []string
	factory := &RemoteSSHTransportFactory{
		SSHPath:       "/usr/bin/ssh",
		SSHConfigPath: filepath.Join(t.TempDir(), "ssh config"),
		commandContext: func(ctx context.Context, path string, args ...string) *exec.Cmd {
			gotPath = path
			gotArgs = append([]string(nil), args...)
			return remoteSSHFakeCommand(ctx)
		},
	}
	transport, err := factory.Start(context.Background(), "lab-linux")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = transport.Close() })
	wantArgs := []string{
		"-F", filepath.Clean(factory.SSHConfigPath),
		"-T", "-o", "RequestTTY=no", "-o", "StrictHostKeyChecking=ask",
		"-o", "ClearAllForwardings=yes", "-o", "PermitLocalCommand=no",
		"-o", "RemoteCommand=none", "--", "lab-linux",
		"reasonix", "remote", "attach", "--stdio",
	}
	if gotPath != factory.SSHPath || !reflect.DeepEqual(gotArgs, wantArgs) {
		t.Fatalf("command = %q %#v, want %q %#v", gotPath, gotArgs, factory.SSHPath, wantArgs)
	}
	request := "{\"jsonrpc\":\"2.0\",\"id\":1}\n"
	if _, err := io.WriteString(transport, request); err != nil {
		t.Fatal(err)
	}
	if err := transport.Stdin().Close(); err != nil {
		t.Fatal(err)
	}
	response, err := io.ReadAll(transport)
	if err != nil {
		t.Fatal(err)
	}
	if string(response) != request {
		t.Fatalf("protocol stdout = %q, want %q", response, request)
	}
	if err := transport.Wait(); err != nil {
		t.Fatal(err)
	}
	if !transport.bootstrapStarted.Load() {
		t.Fatal("first protocol bytes did not mark bootstrap started")
	}
	if strings.Contains(string(response), "stderr diagnostic") {
		t.Fatal("stderr contaminated protocol stdout")
	}
}

func TestRemoteSSHTransportDrainsAndBoundsLargeStderr(t *testing.T) {
	t.Setenv("GO_WANT_REMOTE_SSH_FAKE", "1")
	t.Setenv("REMOTE_SSH_FAKE_MODE", "huge-cli")
	factory := &RemoteSSHTransportFactory{StderrLimit: 4096, commandContext: func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
		return remoteSSHFakeCommand(ctx)
	}}
	transport, err := factory.Start(context.Background(), "host")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.ReadAll(transport)
	waitDone := make(chan error, 1)
	go func() { waitDone <- transport.Wait() }()
	select {
	case err := <-waitDone:
		var failure *RemoteSSHConnectionFailure
		if !errors.As(err, &failure) || failure.Code != RemoteSSHCLINotFound || failure.Stage != RemoteSSHStageBootstrap {
			t.Fatalf("Wait error = %#v, want CLI_NOT_FOUND/bootstrap", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Wait deadlocked while draining large stderr")
	}
	diagnostic := transport.Diagnostic()
	if diagnostic.CapturedBytes > 4096 || diagnostic.TotalBytes < 1<<20 || !diagnostic.Truncated {
		t.Fatalf("diagnostic = %#v", diagnostic)
	}
}

func TestRemoteSSHTransportClassifiesControlledFailures(t *testing.T) {
	tests := []struct {
		mode      string
		wantCode  RemoteSSHFailureCode
		wantStage RemoteSSHFailureStage
	}{
		{"auth", RemoteSSHAuthFailed, RemoteSSHStageAuthentication},
		{"host-key", RemoteSSHHostKeyChanged, RemoteSSHStageHostKey},
		{"cli", RemoteSSHCLINotFound, RemoteSSHStageBootstrap},
		{"generic", RemoteSSHTransportLost, RemoteSSHStageTransport},
	}
	for _, test := range tests {
		t.Run(test.mode, func(t *testing.T) {
			t.Setenv("GO_WANT_REMOTE_SSH_FAKE", "1")
			t.Setenv("REMOTE_SSH_FAKE_MODE", test.mode)
			factory := &RemoteSSHTransportFactory{commandContext: func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
				return remoteSSHFakeCommand(ctx)
			}}
			transport, err := factory.Start(context.Background(), "host")
			if err != nil {
				t.Fatal(err)
			}
			_, readErr := io.ReadAll(transport)
			var readFailure *RemoteSSHConnectionFailure
			if !errors.As(readErr, &readFailure) || readFailure.Code != test.wantCode || readFailure.Stage != test.wantStage {
				t.Fatalf("Read failure = %#v (err %v), want %s/%s", readFailure, readErr, test.wantCode, test.wantStage)
			}
			err = transport.Wait()
			var failure *RemoteSSHConnectionFailure
			if !errors.As(err, &failure) || failure.Code != test.wantCode || failure.Stage != test.wantStage {
				t.Fatalf("failure = %#v (err %v), want %s/%s", failure, err, test.wantCode, test.wantStage)
			}
			for _, raw := range []string{"Permission denied", "REMOTE HOST IDENTIFICATION", "reasonix: command not found"} {
				if strings.Contains(failure.Message, raw) {
					t.Fatalf("safe failure leaked raw stderr: %q", failure.Message)
				}
			}
		})
	}
}

func TestRemoteSSHTransportRejectsAliasInjectionBeforeExec(t *testing.T) {
	for _, alias := range []string{"-oProxyCommand=evil", "host name", "host;evil", "user@host", "host\n-o evil"} {
		t.Run(strings.NewReplacer("/", "_", " ", "_").Replace(alias), func(t *testing.T) {
			var called atomic.Bool
			factory := &RemoteSSHTransportFactory{commandContext: func(context.Context, string, ...string) *exec.Cmd {
				called.Store(true)
				return nil
			}}
			if _, err := factory.Start(context.Background(), alias); err == nil {
				t.Fatal("injection alias accepted")
			}
			if called.Load() {
				t.Fatal("exec builder reached for invalid alias")
			}
		})
	}
}

func TestRemoteSSHTransportSystemSSHMissingIsControlledCLINotFound(t *testing.T) {
	factory := &RemoteSSHTransportFactory{SSHPath: filepath.Join(t.TempDir(), "missing-ssh")}
	_, err := factory.Start(context.Background(), "host")
	var failure *RemoteSSHConnectionFailure
	if !errors.As(err, &failure) || failure.Code != RemoteSSHCLINotFound || failure.Stage != RemoteSSHStageStart {
		t.Fatalf("failure = %#v (err %v), want CLI_NOT_FOUND/ssh_start", failure, err)
	}
	if !strings.Contains(failure.Message, "OpenSSH") {
		t.Fatalf("message = %q, want system OpenSSH context", failure.Message)
	}
}

func TestRemoteSSHTransportCLIMissingAfterProtocolIsTransportLost(t *testing.T) {
	failure := classifyRemoteSSHExit([]byte("reasonix: command not found"), true, nil)
	if failure.Code != RemoteSSHTransportLost || failure.Stage != RemoteSSHStageTransport {
		t.Fatalf("failure = %#v", failure)
	}
}

func TestRemoteSSHHostTransportFactoryAdaptsClientTransport(t *testing.T) {
	t.Setenv("GO_WANT_REMOTE_SSH_FAKE", "1")
	t.Setenv("REMOTE_SSH_FAKE_MODE", "protocol")
	sshFactory := &RemoteSSHTransportFactory{commandContext: func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
		return remoteSSHFakeCommand(ctx)
	}}
	entry, err := NewRemoteHostEntry("saved-host", "Saved Host")
	if err != nil {
		t.Fatal(err)
	}
	entry.SSHConfigPath = filepath.Join(t.TempDir(), "ssh config")
	factory, err := NewRemoteSSHHostTransportFactory(sshFactory, entry)
	if err != nil {
		t.Fatal(err)
	}
	if factory.EntryID != entry.ID || factory.Alias != entry.Alias || factory.SSH.SSHConfigPath != entry.SSHConfigPath {
		t.Fatalf("bound factory = %#v", factory)
	}
	transport, err := factory.Open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if transport == nil {
		t.Fatal("Open returned nil transport")
	}
	_ = transport.Close()
}

func TestRemoteSSHTransportAskPassEnvironmentIsControlled(t *testing.T) {
	t.Setenv("GO_WANT_REMOTE_SSH_FAKE", "1")
	t.Setenv("REMOTE_SSH_FAKE_MODE", "protocol")
	t.Setenv("SSH_ASKPASS", "/untrusted/helper")
	broker, err := StartRemoteAskPassBroker(context.Background(), time.Minute, func(context.Context, RemoteAskPassPrompt) (RemoteAskPassAnswer, error) {
		return RemoteAskPassAnswer{Accepted: false}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = broker.Close() })
	helper := filepath.Join(t.TempDir(), "reasonix-desktop")
	var command *exec.Cmd
	factory := &RemoteSSHTransportFactory{AskPass: broker, AskPassHelper: helper, commandContext: func(ctx context.Context, _ string, _ ...string) *exec.Cmd {
		command = remoteSSHFakeCommand(ctx)
		return command
	}}
	transport, err := factory.Start(context.Background(), "host")
	if err != nil {
		t.Fatal(err)
	}
	env := parseEnvironmentMap(command.Env)
	if env["SSH_ASKPASS"] != helper || env["SSH_ASKPASS_REQUIRE"] != "force" || env[remoteAskPassModeEnv] != "1" {
		t.Fatalf("controlled AskPass environment missing: %#v", env)
	}
	if env["SSH_ASKPASS"] == "/untrusted/helper" {
		t.Fatal("inherited SSH_ASKPASS survived")
	}
	_ = transport.Close()
}

func TestRemoteSSHTransportRejectsRelativeConfigPath(t *testing.T) {
	var called atomic.Bool
	factory := &RemoteSSHTransportFactory{SSHConfigPath: "relative/config", commandContext: func(context.Context, string, ...string) *exec.Cmd {
		called.Store(true)
		return nil
	}}
	if _, err := factory.Start(context.Background(), "host"); err == nil {
		t.Fatal("relative -F config accepted")
	}
	if called.Load() {
		t.Fatal("exec reached for relative config")
	}
}

func remoteSSHFakeCommand(ctx context.Context) *exec.Cmd {
	return exec.CommandContext(ctx, os.Args[0], "-test.run=^TestRemoteSSHFakeProcess$")
}

func TestRemoteSSHFakeProcess(t *testing.T) {
	if os.Getenv("GO_WANT_REMOTE_SSH_FAKE") != "1" {
		return
	}
	switch os.Getenv("REMOTE_SSH_FAKE_MODE") {
	case "protocol":
		_, _ = fmt.Fprintln(os.Stderr, "stderr diagnostic only")
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			_, _ = fmt.Fprintln(os.Stdout, scanner.Text())
		}
		os.Exit(0)
	case "huge-cli":
		_, _ = io.WriteString(os.Stderr, "reasonix: command not found\n")
		chunk := strings.Repeat("x", 32<<10)
		for i := 0; i < 64; i++ {
			_, _ = io.WriteString(os.Stderr, chunk)
		}
		os.Exit(127)
	case "auth":
		_, _ = io.WriteString(os.Stderr, "Permission denied (publickey,password).\n")
	case "host-key":
		_, _ = io.WriteString(os.Stderr, "WARNING: REMOTE HOST IDENTIFICATION HAS CHANGED!\n")
	case "cli":
		_, _ = io.WriteString(os.Stderr, "sh: reasonix: command not found\n")
	case "generic":
		_, _ = io.WriteString(os.Stderr, "Connection reset by peer\n")
	case "hang":
		_, _ = fmt.Fprintln(os.Stdout, "ready")
		time.Sleep(10 * time.Minute)
	default:
		_, _ = io.WriteString(os.Stderr, "unknown fake mode\n")
	}
	os.Exit(255)
}
