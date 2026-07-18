package lifecycle

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"reasonix/internal/remote/protocol"
	"reasonix/internal/remote/service"
)

type Options struct {
	ReasonixHome   string
	UnitPath       string
	SocketPath     string
	ExecutablePath string
	CLIBuildID     protocol.BuildID
	Runner         CommandRunner
	DaemonProbe    DaemonProbe
}

type SystemdManager struct {
	profile      InstallProfile
	unitPath     string
	socketPath   string
	executable   string
	managedRoot  string
	managedDir   string
	binaryPath   string
	manifestPath string
	lockPath     string
	cliBuildID   protocol.BuildID
	runner       CommandRunner
	probe        DaemonProbe
	uid          int
	sourceOpener executableSourceOpener
}

var _ Manager = (*SystemdManager)(nil)

// New constructs the production backend for the current platform. V1 refuses
// non-Linux hosts explicitly; it never falls back to nohup or another service
// manager.
func New(options Options) (Manager, error) {
	return newForPlatform(runtime.GOOS, options)
}

func newForPlatform(platform string, options Options) (Manager, error) {
	if platform != "linux" {
		return nil, ErrUnsupportedPlatform
	}
	return newSystemd(options, currentUID())
}

// newSystemd remains free of Linux-only Go APIs so the package and public
// contract cross-compile on future platforms. The uid is an internal test seam;
// production callers cannot override the actual process identity.
func newSystemd(options Options, uid int) (*SystemdManager, error) {
	home, err := cleanAbsoluteHome(options.ReasonixHome)
	if err != nil {
		return nil, err
	}
	if err := options.CLIBuildID.Validate(); err != nil {
		return nil, fmt.Errorf("CLI Build ID: %w", err)
	}
	profile := InstallProfile{ReasonixHome: home, ID: profileID(home)}

	unitPath := strings.TrimSpace(options.UnitPath)
	if unitPath == "" {
		configDir, err := os.UserConfigDir()
		if err != nil || strings.TrimSpace(configDir) == "" {
			return nil, errors.New("cannot resolve systemd user unit directory")
		}
		unitPath = filepath.Join(configDir, "systemd", "user", service.UnitName)
	}
	unitPath, err = cleanAbsolutePath("systemd user unit", unitPath)
	if err != nil {
		return nil, err
	}

	socketPath := strings.TrimSpace(options.SocketPath)
	if socketPath == "" {
		runtimeDir := strings.TrimSpace(os.Getenv("XDG_RUNTIME_DIR"))
		if runtimeDir == "" {
			return nil, errors.New("XDG_RUNTIME_DIR is required for Reasonix Remote lifecycle")
		}
		socketPath = filepath.Join(runtimeDir, service.SocketDirName, service.SocketFileName)
	}
	socketPath, err = cleanAbsolutePath("Remote socket", socketPath)
	if err != nil {
		return nil, err
	}

	executable := strings.TrimSpace(options.ExecutablePath)
	if executable != "" {
		executable, err = cleanAbsolutePath("current executable test source", executable)
		if err != nil {
			return nil, err
		}
	}

	runner := options.Runner
	if runner == nil {
		runner = ExecRunner{}
	}
	if uid < 0 {
		return nil, errors.New("current user uid is unavailable")
	}
	if uid == 0 {
		return nil, ErrRootUnsupported
	}

	managedRoot := filepath.Join(home, "remote")
	managedDir := filepath.Join(managedRoot, "bin")
	manager := &SystemdManager{
		profile:      profile,
		unitPath:     unitPath,
		socketPath:   socketPath,
		executable:   executable,
		managedRoot:  managedRoot,
		managedDir:   managedDir,
		binaryPath:   filepath.Join(managedDir, "reasonix"),
		manifestPath: filepath.Join(managedDir, ManifestName),
		lockPath:     unitPath + ".lock",
		cliBuildID:   options.CLIBuildID,
		runner:       runner,
		probe:        options.DaemonProbe,
		uid:          uid,
	}
	if executable != "" {
		manager.sourceOpener = func() (*os.File, os.FileInfo, error) { return openExplicitExecutable(executable) }
	} else {
		manager.sourceOpener = openCurrentExecutable
	}
	return manager, nil
}

func cleanAbsoluteHome(path string) (string, error) {
	clean, err := cleanAbsolutePath("Reasonix Home", path)
	if err != nil {
		return "", err
	}
	volume := filepath.VolumeName(clean)
	if clean == string(filepath.Separator) || clean == volume+string(filepath.Separator) {
		return "", errors.New("Reasonix Home cannot be a filesystem root")
	}
	return clean, nil
}

func cleanAbsolutePath(label, path string) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", fmt.Errorf("%s is required", label)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", label, err)
	}
	abs = filepath.Clean(abs)
	if !filepath.IsAbs(abs) {
		return "", fmt.Errorf("%s must be absolute", label)
	}
	return abs, nil
}

func profileID(home string) string {
	sum := sha256.Sum256([]byte(home))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func (m *SystemdManager) run(ctx context.Context, name string, args ...string) (CommandOutput, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return CommandOutput{}, err
	}
	out, err := m.runner.Run(ctx, name, args...)
	if err != nil {
		return out, &CommandError{Name: name, Args: append([]string(nil), args...), Output: out, Err: err}
	}
	return out, nil
}

func (m *SystemdManager) systemctl(ctx context.Context, args ...string) (CommandOutput, error) {
	return m.run(ctx, "systemctl", append([]string{"--user"}, args...)...)
}

// ExecRunner is the production no-shell command runner.
type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, name string, args ...string) (CommandOutput, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	cmd := exec.CommandContext(ctx, name, args...)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	exitCode := 0
	if err != nil {
		exitCode = -1
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			exitCode = exitErr.ExitCode()
		}
	}
	return CommandOutput{Stdout: stdout.String(), Stderr: stderr.String(), ExitCode: exitCode}, err
}

func uidText(uid int) string { return strconv.Itoa(uid) }
