package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"unicode"

	"reasonix/internal/config"
	"reasonix/internal/remote/daemonprobe"
	"reasonix/internal/remote/lifecycle"
	"reasonix/internal/remote/protocol"
	"reasonix/internal/remote/service"
)

type remoteLifecycleManagerFactory func(string) (lifecycle.Manager, error)

var newRemoteLifecycleManager remoteLifecycleManagerFactory = newProductionRemoteLifecycleManager

func newProductionRemoteLifecycleManager(version string) (lifecycle.Manager, error) {
	buildID, err := currentRemoteBuildID(version)
	if err != nil {
		return nil, err
	}
	endpoint, err := service.DefaultEndpoint()
	if err != nil {
		return nil, err
	}
	unitPath, err := endpoint.UnitPath()
	if err != nil {
		return nil, err
	}
	socketPath, err := endpoint.SocketPath()
	if err != nil {
		return nil, err
	}
	probe, err := daemonprobe.New(endpoint)
	if err != nil {
		return nil, err
	}
	return lifecycle.New(lifecycle.Options{
		ReasonixHome: config.ReasonixHomeDir(),
		UnitPath:     unitPath,
		SocketPath:   socketPath,
		CLIBuildID:   buildID,
		DaemonProbe:  probe,
	})
}

func remoteLifecycleActionCommand(command string, args []string, version string, streams remoteCommandIO) int {
	if streams.stderr == nil {
		return 1
	}
	if len(args) != 0 {
		remoteLifecycleUsageError(streams.stderr, command, "this command accepts no arguments")
		return 2
	}
	manager, err := newRemoteLifecycleManager(version)
	if err != nil {
		remoteLifecycleError(streams.stderr, command, err)
		return 1
	}
	if manager == nil {
		remoteLifecycleError(streams.stderr, command, errors.New("lifecycle manager is unavailable"))
		return 1
	}

	ctx, stop := remoteLifecycleContext()
	defer stop()
	var result lifecycle.ActionResult
	switch command {
	case "install":
		result, err = manager.Install(ctx)
	case "start":
		result, err = manager.Start(ctx)
	case "stop":
		result, err = manager.Stop(ctx)
	case "restart":
		result, err = manager.Restart(ctx)
	case "uninstall":
		result, err = manager.Uninstall(ctx)
	default:
		remoteLifecycleError(streams.stderr, command, errors.New("unsupported lifecycle action"))
		return 1
	}
	if err != nil {
		remoteLifecycleError(streams.stderr, command, err)
		return 1
	}
	state := "no changes"
	if result.Changed {
		state = "completed"
	}
	fmt.Fprintf(streams.stderr, "reasonix remote %s: %s\n", command, state)
	renderRemoteDiagnostics(streams.stderr, result.Diagnostics)
	return 0
}

func remoteStatusCommand(args []string, version string, streams remoteCommandIO) int {
	if streams.stdout == nil || streams.stderr == nil {
		return 1
	}
	jsonOutput := false
	switch {
	case len(args) == 0:
	case len(args) == 1 && args[0] == "--json":
		jsonOutput = true
	default:
		remoteLifecycleUsageError(streams.stderr, "status", "only the optional --json flag is supported")
		return 2
	}
	manager, err := newRemoteLifecycleManager(version)
	if err != nil {
		remoteLifecycleError(streams.stderr, "status", err)
		return 1
	}
	if manager == nil {
		remoteLifecycleError(streams.stderr, "status", errors.New("lifecycle manager is unavailable"))
		return 1
	}
	ctx, stop := remoteLifecycleContext()
	defer stop()
	status, err := manager.Status(ctx)
	if err != nil {
		remoteLifecycleError(streams.stderr, "status", err)
		return 1
	}
	if jsonOutput {
		if err := writeRemoteStatusJSON(streams.stdout, status); err != nil {
			remoteLifecycleError(streams.stderr, "status", err)
			return 1
		}
	} else {
		renderRemoteStatus(streams.stdout, status)
	}
	renderRemoteDiagnostics(streams.stderr, status.Diagnostics)
	return 0
}

func remoteDoctorCommand(args []string, version string, streams remoteCommandIO) int {
	if streams.stderr == nil {
		return 1
	}
	if len(args) != 0 {
		remoteLifecycleUsageError(streams.stderr, "doctor", "this command accepts no arguments")
		return 2
	}
	manager, err := newRemoteLifecycleManager(version)
	if err != nil {
		remoteLifecycleError(streams.stderr, "doctor", err)
		return 1
	}
	if manager == nil {
		remoteLifecycleError(streams.stderr, "doctor", errors.New("lifecycle manager is unavailable"))
		return 1
	}
	ctx, stop := remoteLifecycleContext()
	defer stop()
	report, err := manager.Doctor(ctx)
	if err != nil {
		remoteLifecycleError(streams.stderr, "doctor", err)
		return 1
	}
	renderRemoteDoctor(streams.stderr, report)
	if !report.Healthy {
		return 1
	}
	return 0
}

func remoteLogsCommand(args []string, version string, streams remoteCommandIO) int {
	if streams.stdout == nil || streams.stderr == nil {
		return 1
	}
	options, err := parseRemoteLogsOptions(args)
	if err != nil {
		remoteLifecycleUsageError(streams.stderr, "logs", err.Error())
		return 2
	}
	manager, err := newRemoteLifecycleManager(version)
	if err != nil {
		remoteLifecycleError(streams.stderr, "logs", err)
		return 1
	}
	if manager == nil {
		remoteLifecycleError(streams.stderr, "logs", errors.New("lifecycle manager is unavailable"))
		return 1
	}
	ctx, stop := remoteLifecycleContext()
	defer stop()
	result, err := manager.Logs(ctx, options)
	if err != nil {
		remoteLifecycleError(streams.stderr, "logs", err)
		return 1
	}
	if _, err := io.WriteString(streams.stdout, result.Output); err != nil {
		remoteLifecycleError(streams.stderr, "logs", err)
		return 1
	}
	return 0
}

func parseRemoteLogsOptions(args []string) (lifecycle.LogsOptions, error) {
	var options lifecycle.LogsOptions
	var linesSeen, sinceSeen bool
	for index := 0; index < len(args); index++ {
		argument := args[index]
		name, value, hasValue := strings.Cut(argument, "=")
		switch name {
		case "--lines":
			if linesSeen {
				return lifecycle.LogsOptions{}, errors.New("--lines may be specified only once")
			}
			linesSeen = true
			if !hasValue {
				index++
				if index >= len(args) {
					return lifecycle.LogsOptions{}, errors.New("--lines requires a value")
				}
				value = args[index]
			}
			if value == "" || strings.IndexFunc(value, func(r rune) bool { return r < '0' || r > '9' }) >= 0 {
				return lifecycle.LogsOptions{}, errors.New("--lines must be a decimal integer between 0 and 1000000")
			}
			lines, parseErr := strconv.ParseInt(value, 10, 32)
			if parseErr != nil || lines > 1_000_000 {
				return lifecycle.LogsOptions{}, errors.New("--lines must be a decimal integer between 0 and 1000000")
			}
			options.Lines = int(lines)
		case "--since":
			if sinceSeen {
				return lifecycle.LogsOptions{}, errors.New("--since may be specified only once")
			}
			sinceSeen = true
			if !hasValue {
				index++
				if index >= len(args) {
					return lifecycle.LogsOptions{}, errors.New("--since requires a value")
				}
				value = args[index]
			}
			if strings.TrimSpace(value) == "" {
				return lifecycle.LogsOptions{}, errors.New("--since requires a non-empty value")
			}
			if strings.IndexFunc(value, unicode.IsControl) >= 0 {
				return lifecycle.LogsOptions{}, errors.New("--since contains a control character")
			}
			options.Since = value
		default:
			return lifecycle.LogsOptions{}, fmt.Errorf("unsupported argument %q", argument)
		}
	}
	return options, nil
}

func remoteLifecycleContext() (context.Context, context.CancelFunc) {
	return signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
}

func remoteLifecycleUsageError(writer io.Writer, command, message string) {
	fmt.Fprintf(writer, "reasonix remote %s: %s\n", command, message)
	switch command {
	case "status":
		fmt.Fprintln(writer, "Usage: reasonix remote status [--json]")
	case "logs":
		fmt.Fprintln(writer, "Usage: reasonix remote logs [--lines N] [--since VALUE]")
	default:
		fmt.Fprintf(writer, "Usage: reasonix remote %s\n", command)
	}
}

func remoteLifecycleError(writer io.Writer, command string, err error) {
	fmt.Fprintf(writer, "reasonix remote %s: %v\n", command, err)
}

func renderRemoteDiagnostics(writer io.Writer, diagnostics []lifecycle.Diagnostic) {
	for _, item := range diagnostics {
		fmt.Fprintf(writer, "[%s] %s\n", item.Severity, item.Message)
		if item.Suggestion != "" {
			fmt.Fprintf(writer, "  suggestion: %s\n", item.Suggestion)
		}
	}
}

func writeRemoteStatusJSON(writer io.Writer, status lifecycle.Status) error {
	encoded, err := json.Marshal(status)
	if err != nil {
		return fmt.Errorf("encode status: %w", err)
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &object); err != nil {
		return fmt.Errorf("encode status: %w", err)
	}
	for _, key := range []string{"cliBuildId", "installedBuildId", "daemonBuildId"} {
		if _, present := object[key]; !present {
			object[key] = json.RawMessage("null")
		}
	}
	encoder := json.NewEncoder(writer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(object); err != nil {
		return fmt.Errorf("write status: %w", err)
	}
	return nil
}

func renderRemoteStatus(writer io.Writer, status lifecycle.Status) {
	fmt.Fprintln(writer, "Reasonix Remote status")
	fmt.Fprintf(writer, "Platform: %s\n", emptyRemoteStatusValue(status.Platform))
	fmt.Fprintf(writer, "Reasonix Home: %s\n", emptyRemoteStatusValue(status.Profile.ReasonixHome))
	fmt.Fprintf(writer, "CLI Build ID: %s\n", formatRemoteBuildID(&status.CLIBuildID))
	fmt.Fprintf(writer, "Installed Build ID: %s\n", formatRemoteBuildID(status.InstalledBuildID))
	fmt.Fprintf(writer, "Daemon Build ID: %s\n", formatRemoteBuildID(status.DaemonBuildID))
	fmt.Fprintf(writer, "Service enabled: %s\n", yesNo(status.Unit.Enabled))
	fmt.Fprintf(writer, "Service active: %s\n", yesNo(status.Unit.Active))
	fmt.Fprintf(writer, "Socket secure: %s\n", yesNo(status.Socket.Secure))
	linger := "unknown"
	if status.Lingering.Known {
		linger = "disabled"
		if status.Lingering.Enabled {
			linger = "enabled"
		}
	}
	fmt.Fprintf(writer, "Lingering: %s\n", linger)
}

func renderRemoteDoctor(writer io.Writer, report lifecycle.DoctorReport) {
	state := "healthy"
	if !report.Healthy {
		state = "unhealthy"
	}
	fmt.Fprintf(writer, "Reasonix Remote doctor: %s\n", state)
	for _, check := range report.Checks {
		fmt.Fprintf(writer, "[%s] %s: %s\n", check.State, check.Name, check.Detail)
		if check.Suggestion != "" {
			fmt.Fprintf(writer, "  suggestion: %s\n", check.Suggestion)
		}
	}
	renderRemoteDiagnostics(writer, report.Status.Diagnostics)
}

func formatRemoteBuildID(buildID *protocol.BuildID) string {
	if buildID == nil {
		return "unavailable"
	}
	return fmt.Sprintf("%s / %s / protocol %s / %s", buildID.ProductVersion, buildID.SourceRevision, buildID.ProtocolVersion, buildID.SchemaHash)
}

func emptyRemoteStatusValue(value string) string {
	if strings.TrimSpace(value) == "" {
		return "unavailable"
	}
	return value
}

func yesNo(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}
