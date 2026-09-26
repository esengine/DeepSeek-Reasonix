package bootstrap

import (
	"context"
	"fmt"
	"strings"

	"reasonix/internal/platform/releaseasset"
	"reasonix/internal/platform/remote/sftpfs"
)

// remoteOS is everything this package has to say to a machine that is not this
// one: the shell it speaks, where its files live, how a process is started
// there and found again. Keeping the difference behind one interface is what
// stops it from becoming a branch at every call site.
type remoteOS interface {
	// Executable is what the CLI is called on that platform.
	Executable() string
	// Home is the user's directory, spelled the way the file layer addresses
	// it — which on Windows is not how its own shell spells it.
	Home(ctx context.Context, conn Conn, fs *sftpfs.FS) (string, error)
	Paths(home, workspace string) StatePaths
	Launch(spec LaunchSpec, p StatePaths) string
	Fetch(d releaseasset.CLIDownload, dir, bin string) string
	Downloader() string
	Alive(pid int, p StatePaths) string
	Stop(pid int, p StatePaths) string
	Logs(logFile string, n int) string
	Locate(uploadedBin string, flags []string) string
	// NPMVersion asks npm what it is. Empty output means no npm, which is the
	// first fork in whether a kernel can be installed here at all.
	NPMVersion() string
	// NativePath turns a path the file layer addresses into the one that
	// machine's own kernel spells it with. The same file, two strings.
	NativePath(p string) string
}

// remoteFor resolves which machine this is and where its files live. Every
// entry point needs both before it can name a single path, so the two answers
// are fetched together rather than rediscovered at each one.
func remoteFor(ctx context.Context, conn Conn, fs *sftpfs.FS) (target remoteOS, goos, goarch, home string, err error) {
	goos, goarch, err = detectPlatform(ctx, conn)
	if err != nil {
		return nil, "", "", "", err
	}
	target = osFor(goos)
	home, err = target.Home(ctx, conn, fs)
	if err != nil {
		return nil, "", "", "", err
	}
	return target, goos, goarch, home, nil
}

func osFor(goos string) remoteOS {
	if goos == "windows" {
		return windowsShell{}
	}
	return posixShell{}
}

// detectPlatform asks the machine what it is. uname answers on everything
// POSIX; a Windows host is asked through PowerShell, the one interpreter every
// windowsShell command already runs in, because OpenSSH there may start cmd,
// PowerShell or Git Bash — and Git Bash answers uname with MSYS_NT.
func detectPlatform(ctx context.Context, conn Conn) (goos, goarch string, err error) {
	if res, execErr := conn.Exec(ctx, "uname -sm"); execErr == nil {
		if goos, goarch, perr := ParseUname(string(res.Stdout)); perr == nil {
			return goos, goarch, nil
		}
	}
	res, execErr := conn.Exec(ctx, windowsIdentity)
	if execErr != nil {
		return "", "", fmt.Errorf("bootstrap: identify remote: %w", execErr)
	}
	return ParseWindowsEnv(string(res.Stdout))
}

var windowsIdentity = psCommand("$env:OS + ' ' + $env:PROCESSOR_ARCHITECTURE")

// ParseWindowsEnv reads the OS and PROCESSOR_ARCHITECTURE variables, space
// separated. Anything else — including a shell that ran nothing — is not a
// platform, and is exactly as unsupported as one that named the wrong OS.
func ParseWindowsEnv(out string) (goos, goarch string, err error) {
	fields := strings.Fields(strings.TrimSpace(out))
	if len(fields) < 2 || !strings.EqualFold(fields[0], "Windows_NT") {
		return "", "", fmt.Errorf("%w: Windows identity probe answered %q", ErrUnsupportedRemote, strings.TrimSpace(out))
	}
	switch strings.ToUpper(fields[1]) {
	case "AMD64":
		return "windows", "amd64", nil
	case "ARM64":
		return "windows", "arm64", nil
	default:
		return "", "", fmt.Errorf("%w: architecture %q", ErrUnsupportedRemote, fields[1])
	}
}
