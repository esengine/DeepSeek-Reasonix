package bootstrap

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path"
	"strings"

	"golang.org/x/mod/semver"

	"reasonix/internal/remote/protocol"
	"reasonix/internal/remote/sftpfs"
)

// WorkbenchOptions configures provisioning of the exact CLI build required by
// Remote Workbench. FetchBinary runs on the local client and returns a verified
// official release executable for the requested remote platform.
type WorkbenchOptions struct {
	Install       string
	LocalBinary   string
	LocalGOOS     string
	LocalGOARCH   string
	ExpectedBuild protocol.BuildID
	FetchBinary   func(context.Context, string, string, string) ([]byte, error)
	Progress      func(step, detail string)
}

func (o WorkbenchOptions) progress(step, detail string) {
	if o.Progress != nil {
		o.Progress(step, detail)
	}
}

// WorkbenchCLI identifies a compatible remote executable. Path is always an
// absolute remote path so the attach command does not depend on non-interactive
// shell PATH configuration.
type WorkbenchCLI struct {
	Path      string
	Installed bool
}

// EnsureWorkbenchCLI locates or provisions the exact CLI build required by the
// Desktop's frozen Remote Workbench protocol. Managed binaries live in a
// build-specific directory, so concurrent versions never overwrite each other.
func EnsureWorkbenchCLI(ctx context.Context, conn Conn, opts WorkbenchOptions) (WorkbenchCLI, error) {
	if err := opts.ExpectedBuild.Validate(); err != nil {
		return WorkbenchCLI{}, fmt.Errorf("workbench bootstrap: invalid expected build: %w", err)
	}
	opts.progress("detect", "")
	pathBinary, pathErr := probeWorkbenchCLI(ctx, conn, "", opts.ExpectedBuild)
	if pathBinary != "" {
		opts.progress("reuse", pathBinary)
		return WorkbenchCLI{Path: pathBinary}, nil
	}

	fs, err := conn.SFTP()
	if err != nil {
		return WorkbenchCLI{}, err
	}
	home, err := fs.RealPath(ctx, "~")
	if err != nil {
		return WorkbenchCLI{}, fmt.Errorf("workbench bootstrap: resolve remote home: %w", err)
	}
	managedPath := workbenchBinPath(home, opts.ExpectedBuild)

	// Reuse a previously provisioned immutable build when PATH does not already
	// provide an exact match. A stale CLI is never allowed to answer the strict
	// initialize handshake by accident.
	if bin, _ := probeWorkbenchCLI(ctx, conn, managedPath, opts.ExpectedBuild); bin != "" {
		opts.progress("reuse", bin)
		return WorkbenchCLI{Path: bin}, nil
	}

	strategy := strings.ToLower(strings.TrimSpace(opts.Install))
	if strategy == "" {
		strategy = InstallAuto
	}
	if strategy == InstallNever {
		return WorkbenchCLI{}, fmt.Errorf("workbench bootstrap: no compatible remote Reasonix CLI and serve_install = never: %w", pathErr)
	}
	opts.progress("install", strategy)
	platform, err := conn.Exec(ctx, "uname -sm")
	if err != nil {
		return WorkbenchCLI{}, fmt.Errorf("workbench bootstrap: uname: %w", err)
	}
	goos, goarch, err := ParseUname(string(platform.Stdout))
	if err != nil {
		return WorkbenchCLI{}, err
	}

	var attempts []error
	tryUpload := func(binary []byte, source string) (WorkbenchCLI, bool) {
		if len(binary) == 0 {
			attempts = append(attempts, fmt.Errorf("%s returned an empty binary", source))
			return WorkbenchCLI{}, false
		}
		if err := installWorkbenchBinary(ctx, fs, managedPath, binary); err != nil {
			attempts = append(attempts, fmt.Errorf("%s upload: %w", source, err))
			return WorkbenchCLI{}, false
		}
		bin, probeErr := probeWorkbenchCLI(ctx, conn, managedPath, opts.ExpectedBuild)
		if bin == "" {
			attempts = append(attempts, fmt.Errorf("%s verification: %w", source, probeErr))
			return WorkbenchCLI{}, false
		}
		return WorkbenchCLI{Path: bin, Installed: true}, true
	}
	localBinary := func() ([]byte, error) {
		if strings.TrimSpace(opts.LocalBinary) == "" {
			return nil, errors.New("no packaged local Reasonix CLI is available")
		}
		if opts.LocalGOOS != goos || opts.LocalGOARCH != goarch {
			return nil, fmt.Errorf("local binary is %s/%s but remote is %s/%s", opts.LocalGOOS, opts.LocalGOARCH, goos, goarch)
		}
		return os.ReadFile(opts.LocalBinary)
	}
	tryLocal := func() (WorkbenchCLI, bool) {
		binary, readErr := localBinary()
		if readErr != nil {
			attempts = append(attempts, fmt.Errorf("packaged CLI: %w", readErr))
			return WorkbenchCLI{}, false
		}
		return tryUpload(binary, "packaged CLI")
	}
	tryRelease := func() (WorkbenchCLI, bool) {
		if opts.FetchBinary == nil {
			attempts = append(attempts, errors.New("official release downloader is unavailable"))
			return WorkbenchCLI{}, false
		}
		binary, fetchErr := opts.FetchBinary(ctx, opts.ExpectedBuild.ProductVersion, goos, goarch)
		if fetchErr != nil {
			attempts = append(attempts, fmt.Errorf("official %s/%s CLI: %w", goos, goarch, fetchErr))
			return WorkbenchCLI{}, false
		}
		return tryUpload(binary, "official release CLI")
	}
	tryNPM := func() (WorkbenchCLI, bool) {
		bin, installErr := installWorkbenchViaNPM(ctx, conn, opts.ExpectedBuild)
		if installErr != nil {
			attempts = append(attempts, installErr)
			return WorkbenchCLI{}, false
		}
		return WorkbenchCLI{Path: bin, Installed: true}, true
	}

	switch strategy {
	case InstallUpload:
		if result, ok := tryLocal(); ok {
			return result, nil
		}
	case InstallNPM:
		if result, ok := tryNPM(); ok {
			return result, nil
		}
	case InstallAuto:
		if opts.LocalGOOS == goos && opts.LocalGOARCH == goarch {
			if result, ok := tryLocal(); ok {
				return result, nil
			}
		}
		if result, ok := tryRelease(); ok {
			return result, nil
		}
		if result, ok := tryNPM(); ok {
			return result, nil
		}
	default:
		return WorkbenchCLI{}, fmt.Errorf("workbench bootstrap: unsupported install strategy %q", strategy)
	}
	return WorkbenchCLI{}, fmt.Errorf("workbench bootstrap could not provision a compatible remote CLI: %w", errors.Join(attempts...))
}

func workbenchBinPath(home string, build protocol.BuildID) string {
	raw, _ := json.Marshal(build)
	digest := sha256.Sum256(raw)
	key := hex.EncodeToString(digest[:8])
	return path.Join(remoteDir(home), "workbench", key, "reasonix")
}

func installWorkbenchBinary(ctx context.Context, fs *sftpfs.FS, destination string, binary []byte) error {
	if err := fs.MkdirAll(ctx, path.Dir(destination)); err != nil {
		return err
	}
	if err := fs.WriteFileAtomic(ctx, destination, binary, 0o755); err != nil {
		return err
	}
	return nil
}

func probeWorkbenchCLI(ctx context.Context, conn Conn, candidate string, expected protocol.BuildID) (string, error) {
	res, err := conn.Exec(ctx, WorkbenchProbeCommand(candidate))
	if err != nil {
		return "", err
	}
	lines := strings.Split(strings.TrimSpace(string(res.Stdout)), "\n")
	if len(lines) < 2 || strings.TrimSpace(lines[0]) == "" {
		return "", errors.New("compatible reasonix command was not found")
	}
	var actual protocol.BuildID
	if err := json.Unmarshal([]byte(lines[len(lines)-1]), &actual); err != nil {
		return "", errors.New("remote reasonix does not expose a Workbench build ID")
	}
	if err := protocol.CompareBuildID(expected, actual); err != nil {
		return "", fmt.Errorf("remote reasonix is incompatible: %w", err)
	}
	binary := strings.TrimSpace(lines[0])
	if !path.IsAbs(binary) || path.Clean(binary) != binary {
		return "", fmt.Errorf("remote reasonix resolved to non-absolute path %q", binary)
	}
	return binary, nil
}

// WorkbenchProbeCommand prints the resolved binary path followed by its strict
// Build ID. candidate must be either empty (PATH lookup) or an absolute managed
// path. Every interpolated operand is POSIX-shell quoted.
func WorkbenchProbeCommand(candidate string) string {
	if candidate == "" {
		return `BIN="$(command -v reasonix 2>/dev/null)"; echo "$BIN"; [ -n "$BIN" ] && "$BIN" remote workbench-build-id 2>/dev/null`
	}
	return fmt.Sprintf("BIN=%s; [ -x \"$BIN\" ] || BIN=; echo \"$BIN\"; [ -n \"$BIN\" ] && \"$BIN\" remote workbench-build-id 2>/dev/null", shellQuote(candidate))
}

func installWorkbenchViaNPM(ctx context.Context, conn Conn, expected protocol.BuildID) (string, error) {
	productVersion := strings.TrimSpace(expected.ProductVersion)
	version := strings.TrimPrefix(productVersion, "v")
	if !semver.IsValid(productVersion) || version == "" || strings.ContainsAny(version, " \t\r\n/@") {
		return "", fmt.Errorf("workbench bootstrap: product version %q cannot be installed through npm", expected.ProductVersion)
	}
	res, err := conn.Exec(ctx, "npm i -g "+shellQuote("reasonix@"+version)+" 2>&1")
	if err != nil {
		return "", fmt.Errorf("workbench bootstrap: npm install: %w", err)
	}
	if res.ExitCode != 0 {
		return "", fmt.Errorf("workbench bootstrap: npm install failed: %s", tail(res.Stdout, 400))
	}
	bin, probeErr := probeWorkbenchCLI(ctx, conn, "", expected)
	if bin == "" {
		return "", fmt.Errorf("workbench bootstrap: npm installed an incompatible or unreachable CLI: %w", probeErr)
	}
	return bin, nil
}
