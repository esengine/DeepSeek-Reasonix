package bootstrap

import (
	"context"
	"strings"

	"reasonix/internal/platform/releaseasset"
	"reasonix/internal/platform/remote/sftpfs"
)

// posixShell speaks to Linux and macOS remotes. The commands themselves stay
// where they were: they are the contract this package has always had, and the
// exported forms are what its tests hold it to.
type posixShell struct{}

func (posixShell) Executable() string { return "reasonix" }

func (posixShell) Home(ctx context.Context, _ Conn, fs *sftpfs.FS) (string, error) {
	return fs.RealPath(ctx, "~")
}

func (posixShell) HomeRelative(p string) (string, bool) {
	return strings.CutPrefix(p, "~/")
}

func (posixShell) Absolute(p string) (string, bool) {
	return p, strings.HasPrefix(p, "/")
}

func (posixShell) Paths(home, workspace string) StatePaths {
	return pathsFor(home, workspace)
}

func (posixShell) Launch(spec LaunchSpec, p StatePaths) string {
	return LaunchCommand(spec, p)
}

func (posixShell) Fetch(d releaseasset.CLIDownload, dir, bin string) string {
	return FetchCommand(d, dir, bin)
}

func (posixShell) Alive(pid int, p StatePaths) string {
	return ServeAliveCommand(pid, p)
}

func (posixShell) Stop(pid int, p StatePaths) string {
	return StopCommand(pid, p)
}

func (posixShell) Logs(logFile string, n int) string {
	return LogsCommand(logFile, n)
}

func (posixShell) Locate(uploadedBin string, flags []string) string {
	return LocateCommand(uploadedBin, flags)
}

// NativePath is identity here: the file layer and the shell agree.
func (posixShell) NPMVersion() string { return "npm --version 2>/dev/null" }

// Downloader names what this machine could fetch its own release with, or
// nothing. Both are worth asking about: stock macOS ships curl and no wget,
// and a minimal container image often ships wget and no curl.
func (posixShell) Downloader() string {
	return "if command -v curl >/dev/null 2>&1; then echo curl;" +
		" elif command -v wget >/dev/null 2>&1; then echo wget; fi; exit 0"
}

func (posixShell) NativePath(p string) string { return p }
