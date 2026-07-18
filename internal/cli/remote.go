package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/signal"
	"syscall"

	"reasonix/internal/buildinfo"
	"reasonix/internal/remote/attach"
	"reasonix/internal/remote/hostapp"
	"reasonix/internal/remote/profileconfig"
	"reasonix/internal/remote/protocol"
	"reasonix/internal/remote/service"
)

// remoteCommand is deliberately routed before the CLI's legacy config
// migration and config-load paths. Keep attach's stdout reserved exclusively
// for Remote Protocol frames; lifecycle diagnostics belong on stderr.
func remoteCommand(args []string, version string) int {
	return remoteCommandWithIO(args, version, remoteCommandIO{
		stdin: os.Stdin, stdout: os.Stdout, stderr: os.Stderr,
	})
}

func remoteCommandWithIO(args []string, version string, streams remoteCommandIO) int {
	if len(args) == 0 {
		remoteUsage(streams.stderr)
		return 2
	}
	switch args[0] {
	case "install", "start", "stop", "restart", "uninstall":
		return remoteLifecycleActionCommand(args[0], args[1:], version, streams)
	case "status":
		return remoteStatusCommand(args[1:], version, streams)
	case "doctor":
		return remoteDoctorCommand(args[1:], version, streams)
	case "logs":
		return remoteLogsCommand(args[1:], version, streams)
	case "serve":
		return remoteServeCommand(args[1:], version, streams)
	case "attach":
		return remoteAttachCommand(args[1:], version, streams)
	default:
		fmt.Fprintf(streams.stderr, "unknown reasonix remote command %q\n", args[0])
		remoteUsage(streams.stderr)
		return 2
	}
}

func remoteUsage(writer io.Writer) {
	fmt.Fprint(writer, `Usage:
  reasonix remote install
  reasonix remote start
  reasonix remote stop
  reasonix remote restart
  reasonix remote status [--json]
  reasonix remote doctor
  reasonix remote logs [--lines N] [--since VALUE]
  reasonix remote uninstall
  reasonix remote serve
  reasonix remote attach --stdio
`)
}

type remoteCommandIO struct {
	stdin  io.ReadCloser
	stdout io.Writer
	stderr io.Writer
}

type attachRunner func(context.Context, io.ReadCloser, io.Writer, attach.Options) error

// endpointFactory remains a narrow test seam. The production value is lazy so
// resolving XDG paths cannot precede attach.Run's Desktop Build ID validation.
type endpointFactory func() (attach.Service, error)

type remoteServeRunner func(context.Context, protocol.BuildID, io.Writer) error

type remoteServePrivilegeGuard func() error

var (
	dispatchRemoteCommand                                = remoteCommand
	runAttachBootstrap         attachRunner              = attach.Run
	newAttachEndpoint          endpointFactory           = func() (attach.Service, error) { return &lazySystemEndpoint{}, nil }
	runRemoteServe             remoteServeRunner         = runProductionRemoteServe
	checkRemoteServePrivileges remoteServePrivilegeGuard = productionRemoteServePrivilegeGuard
)

func remoteServeCommand(args []string, version string, streams remoteCommandIO) int {
	if streams.stdout == nil || streams.stderr == nil {
		return 1
	}
	flags := flag.NewFlagSet("remote serve", flag.ContinueOnError)
	flags.SetOutput(streams.stderr)
	flags.Usage = func() { fmt.Fprintln(streams.stderr, "Usage: reasonix remote serve") }
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if flags.NArg() != 0 {
		flags.Usage()
		return 2
	}
	if err := checkRemoteServePrivileges(); err != nil {
		fmt.Fprintln(streams.stderr, "reasonix remote serve:", err)
		return 1
	}
	buildID, err := currentRemoteBuildID(version)
	if err != nil {
		fmt.Fprintln(streams.stderr, "reasonix remote serve:", err)
		return 1
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := runRemoteServe(ctx, buildID, streams.stderr); err != nil {
		fmt.Fprintln(streams.stderr, "reasonix remote serve:", err)
		return 1
	}
	return 0
}

func runProductionRemoteServe(ctx context.Context, buildID protocol.BuildID, stderr io.Writer) error {
	endpoint, err := service.DefaultEndpoint()
	if err != nil {
		return err
	}
	app, err := hostapp.New(ctx, hostapp.Options{
		BuildID: buildID, ProfileResolver: profileconfig.New(), Stderr: stderr,
	})
	if err != nil {
		return err
	}
	defer app.Close()
	return app.Serve(ctx, endpoint)
}

func remoteAttachCommand(args []string, version string, streams remoteCommandIO) int {
	if streams.stdin == nil || streams.stdout == nil || streams.stderr == nil {
		return 1
	}
	flags := flag.NewFlagSet("remote attach", flag.ContinueOnError)
	flags.SetOutput(streams.stderr)
	stdio := flags.Bool("stdio", false, "proxy Remote Protocol over stdin/stdout")
	flags.Usage = func() { fmt.Fprintln(streams.stderr, "Usage: reasonix remote attach --stdio") }
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if !*stdio || flags.NArg() != 0 {
		flags.Usage()
		return 2
	}

	buildID, err := currentRemoteBuildID(version)
	if err != nil {
		fmt.Fprintln(streams.stderr, "reasonix remote attach:", err)
		return 1
	}
	endpoint, err := newAttachEndpoint()
	if err != nil {
		fmt.Fprintln(streams.stderr, "reasonix remote attach:", err)
		return 1
	}
	if endpoint == nil {
		fmt.Fprintln(streams.stderr, "reasonix remote attach: service endpoint is unavailable")
		return 1
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	err = runAttachBootstrap(ctx, streams.stdin, streams.stdout, attach.Options{
		BuildID: buildID,
		Service: endpoint,
		OnDiagnostic: func(diagnostic error) {
			if diagnostic != nil {
				fmt.Fprintln(streams.stderr, "reasonix remote attach:", diagnostic)
			}
		},
	})
	if err != nil {
		fmt.Fprintln(streams.stderr, "reasonix remote attach:", err)
		return 1
	}
	return 0
}

func currentRemoteBuildID(version string) (protocol.BuildID, error) {
	id, err := protocol.NewBuildID(version, buildinfo.Revision())
	if err != nil {
		return protocol.BuildID{}, fmt.Errorf("invalid Remote Build ID: %w", err)
	}
	return id, nil
}

// lazySystemEndpoint delays environment and filesystem path resolution until
// attach.Run has decoded initialize and compared the Desktop/attach Build IDs.
// It never starts, installs, upgrades, or repairs the daemon.
type lazySystemEndpoint struct {
	endpoint *service.Endpoint
	err      error
	loaded   bool
}

func (e *lazySystemEndpoint) load() (*service.Endpoint, error) {
	if e == nil {
		return nil, errors.New("Remote service endpoint is nil")
	}
	if !e.loaded {
		e.endpoint, e.err = service.DefaultEndpoint()
		e.loaded = true
	}
	return e.endpoint, e.err
}

func (e *lazySystemEndpoint) Installed(ctx context.Context) (bool, error) {
	endpoint, err := e.load()
	if err != nil {
		return false, err
	}
	return endpoint.Installed(ctx)
}

func (e *lazySystemEndpoint) Dial(ctx context.Context) (net.Conn, error) {
	endpoint, err := e.load()
	if err != nil {
		return nil, err
	}
	return endpoint.Dial(ctx)
}
