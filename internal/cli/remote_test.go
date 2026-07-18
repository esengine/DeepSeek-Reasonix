package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"reasonix/internal/buildinfo"
	"reasonix/internal/remote/attach"
	"reasonix/internal/remote/protocol"
)

const remoteCLIRevision = "0123456789abcdef0123456789abcdef01234567"

type cliAttachService struct{}

func (cliAttachService) Installed(context.Context) (bool, error) { return true, nil }
func (cliAttachService) Dial(context.Context) (net.Conn, error) {
	return nil, errors.New("not used by the CLI routing test")
}

func isolateRemoteGlobals(t *testing.T) {
	t.Helper()
	previousRevision := buildinfo.SourceRevision
	previousDispatch := dispatchRemoteCommand
	previousRunner := runAttachBootstrap
	previousEndpoint := newAttachEndpoint
	previousServe := runRemoteServe
	previousServePrivileges := checkRemoteServePrivileges
	previousLifecycleManager := newRemoteLifecycleManager
	buildinfo.SourceRevision = remoteCLIRevision
	t.Cleanup(func() {
		buildinfo.SourceRevision = previousRevision
		dispatchRemoteCommand = previousDispatch
		runAttachBootstrap = previousRunner
		newAttachEndpoint = previousEndpoint
		runRemoteServe = previousServe
		checkRemoteServePrivileges = previousServePrivileges
		newRemoteLifecycleManager = previousLifecycleManager
	})
}

func TestRemoteServeUsesStrictBuildIdentityAndKeepsStdoutEmpty(t *testing.T) {
	isolateRemoteGlobals(t)
	checkRemoteServePrivileges = func() error { return nil }
	called := false
	runRemoteServe = func(ctx context.Context, buildID protocol.BuildID, stderr io.Writer) error {
		called = true
		if ctx == nil || buildID.ProductVersion != "v9.8.7" || buildID.SourceRevision != remoteCLIRevision || stderr == nil {
			t.Fatalf("serve arguments ctx=%v buildID=%+v stderr=%v", ctx, buildID, stderr)
		}
		return nil
	}
	var stdout, stderr bytes.Buffer
	if rc := remoteServeCommand(nil, "v9.8.7", remoteCommandIO{stdout: &stdout, stderr: &stderr}); rc != 0 || !called {
		t.Fatalf("remote serve rc=%d called=%v stderr=%q", rc, called, stderr.String())
	}
	if stdout.Len() != 0 || stderr.Len() != 0 {
		t.Fatalf("successful serve stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

func TestRemoteServeRejectsAllFlagsAndArgumentsBeforeStartingHost(t *testing.T) {
	isolateRemoteGlobals(t)
	runRemoteServe = func(context.Context, protocol.BuildID, io.Writer) error {
		t.Fatal("invalid serve arguments started Host")
		return nil
	}
	for _, args := range [][]string{{"--force"}, {"--socket", "/tmp/alternate.sock"}, {"extra"}} {
		var stdout, stderr bytes.Buffer
		if rc := remoteServeCommand(args, "dev", remoteCommandIO{stdout: &stdout, stderr: &stderr}); rc != 2 {
			t.Errorf("args %v rc=%d, want 2", args, rc)
		}
		if stdout.Len() != 0 {
			t.Errorf("args %v wrote stdout %q", args, stdout.String())
		}
	}
}

func TestRemoteCommandRoutesBeforeLegacyMigration(t *testing.T) {
	isolateRemoteGlobals(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	legacy := filepath.Join(home, ".reasonix")
	if err := os.MkdirAll(legacy, 0o700); err != nil {
		t.Fatal(err)
	}
	legacyConfig := filepath.Join(legacy, "config.json")
	if err := os.WriteFile(legacyConfig, []byte(`{"model":"legacy"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	called := false
	dispatchRemoteCommand = func(args []string, version string) int {
		called = true
		if strings.Join(args, " ") != "attach --stdio" || version != "dev" {
			t.Fatalf("Remote dispatch args/version = %v/%q", args, version)
		}
		return 37
	}
	if rc := Run([]string{"remote", "attach", "--stdio"}, "dev"); rc != 37 || !called {
		t.Fatalf("Run Remote rc=%d called=%v", rc, called)
	}
	if _, err := os.Stat(filepath.Join(home, ".config", "reasonix", "config.toml")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Remote command triggered legacy migration: %v", err)
	}
}

func TestRemoteAttachKeepsSuccessfulStdoutProtocolOnly(t *testing.T) {
	isolateRemoteGlobals(t)
	called := false
	runAttachBootstrap = func(_ context.Context, _ io.ReadCloser, stdout io.Writer, options attach.Options) error {
		called = true
		if options.BuildID.SourceRevision != remoteCLIRevision || options.Service == nil {
			t.Fatalf("attach options = %+v", options)
		}
		_, err := io.WriteString(stdout, "{\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{}}\n")
		return err
	}
	newAttachEndpoint = func() (attach.Service, error) { return cliAttachService{}, nil }

	var stdout, stderr bytes.Buffer
	rc := remoteAttachCommand([]string{"--stdio"}, "dev", remoteCommandIO{
		stdin: io.NopCloser(strings.NewReader("")), stdout: &stdout, stderr: &stderr,
	})
	if rc != 0 || !called {
		t.Fatalf("remote attach rc=%d called=%v stderr=%q", rc, called, stderr.String())
	}
	if got := stdout.String(); got != "{\"jsonrpc\":\"2.0\",\"id\":1,\"result\":{}}\n" {
		t.Fatalf("protocol stdout = %q", got)
	}
	if stderr.Len() != 0 {
		t.Fatalf("successful attach stderr = %q", stderr.String())
	}
}

func TestRemoteAttachRequiresExactStdioModeWithoutRunningBootstrap(t *testing.T) {
	isolateRemoteGlobals(t)
	runAttachBootstrap = func(context.Context, io.ReadCloser, io.Writer, attach.Options) error {
		t.Fatal("invalid attach flags reached bootstrap")
		return nil
	}
	newAttachEndpoint = func() (attach.Service, error) {
		t.Fatal("invalid attach flags resolved service")
		return nil, nil
	}
	for _, args := range [][]string{nil, {"--stdio=false"}, {"--stdio", "extra"}, {"--force"}} {
		var stdout, stderr bytes.Buffer
		rc := remoteAttachCommand(args, "dev", remoteCommandIO{
			stdin: io.NopCloser(strings.NewReader("")), stdout: &stdout, stderr: &stderr,
		})
		if rc != 2 {
			t.Errorf("args %v rc=%d, want 2", args, rc)
		}
		if stdout.Len() != 0 {
			t.Errorf("args %v wrote stdout %q", args, stdout.String())
		}
	}
}

func TestCurrentRemoteBuildIDUsesAllFrozenFields(t *testing.T) {
	isolateRemoteGlobals(t)
	id, err := currentRemoteBuildID("v9.8.7")
	if err != nil {
		t.Fatal(err)
	}
	if id.ProductVersion != "v9.8.7" || id.SourceRevision != remoteCLIRevision ||
		id.ProtocolVersion != protocol.ProtocolVersion || id.SchemaHash != protocol.SchemaHash() {
		t.Fatalf("Build ID = %+v", id)
	}
}
