package bootstrap

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"unicode/utf16"

	"reasonix/internal/releaseasset"
	"reasonix/internal/remote/sftpfs"
)

// windowsShell speaks to Windows remotes through PowerShell. Every command is
// handed over as -EncodedCommand — base64 of UTF-16LE, which has no character
// cmd treats as special, so the outer quoting problem is removed rather than
// solved: cmd's escaping rules are where this would have become a security
// bug. Inside the script, PowerShell's single quotes follow the POSIX rule.
type windowsShell struct{}

func (windowsShell) Executable() string { return "reasonix.exe" }

// Home does not ask the shell: OpenSSH on Windows will not resolve ~, but
// every SFTP session starts in the user's directory, so the file layer's own
// idea of "." answers the same question without a second command.
func (windowsShell) Home(ctx context.Context, _ Conn, fs *sftpfs.FS) (string, error) {
	home, err := fs.RealPath(ctx, ".")
	if err != nil {
		return "", fmt.Errorf("bootstrap: resolve remote home: %w", err)
	}
	if strings.TrimSpace(home) == "" {
		return "", fmt.Errorf("%w: the machine reported no home directory", ErrUnsupportedRemote)
	}
	return toSFTPPath(home), nil
}

// Paths keeps the SFTP spelling, which is what the file layer addresses and
// what every state path is stored as. The shell form is derived where a
// command needs it, never the other way round.
func (windowsShell) Paths(home, workspace string) StatePaths {
	return pathsFor(toSFTPPath(home), toSFTPPath(workspace))
}

func (windowsShell) Launch(spec LaunchSpec, p StatePaths) string {
	// The serve runs under a second PowerShell, reached the same encoded way,
	// so its own arguments never pass through a round of shell parsing.
	inner := "& " + psQuote(toShellPath(spec.Bin)) + " serve --addr 127.0.0.1:0 --auth token" +
		" --token-file " + psQuote(toShellPath(p.TokenFile)) +
		" --port-file " + psQuote(toShellPath(p.PortFile)) +
		" --pid-file " + psQuote(toShellPath(p.PidFile)) +
		windowsBrokerArgs(spec, p) +
		" *>> " + psQuote(toShellPath(p.LogFile))

	// OpenSSH ends a session's children with the session, Start-Process included
	// — a serve launched that way was gone before its port file appeared. What
	// the WMI service creates has no such parent; this is the setsid of here.
	launch := "powershell -NoProfile -NonInteractive -EncodedCommand " + encodePS(inner)
	outer := strings.Join([]string{
		"$ErrorActionPreference='Stop'",
		"New-Item -ItemType Directory -Force -Path " + psQuote(toShellPath(p.Dir)) + " | Out-Null",
		"Remove-Item -Force -ErrorAction SilentlyContinue -LiteralPath " +
			psQuote(toShellPath(p.PortFile)) + "," + psQuote(toShellPath(p.PidFile)),
		"$r = Invoke-CimMethod -ClassName Win32_Process -MethodName Create -Arguments @{CommandLine=" +
			psQuote(launch) + "; CurrentDirectory=" + psQuote(toShellPath(spec.Workspace)) + "}",
		"if ($r.ReturnValue -ne 0) { throw \"Win32_Process.Create returned $($r.ReturnValue)\" }",
		// The wrapper's pid. Serve writes its own to --pid-file, which is what
		// everything downstream treats as authoritative.
		"$r.ProcessId",
	}, "; ")
	return psCommand(outer)
}

// windowsBrokerArgs is the broker half of the serve command line, empty when
// this launch configured none. The token rides a file rather than argv, which
// any account on the machine can read out of the process list.
func windowsBrokerArgs(spec LaunchSpec, p StatePaths) string {
	if spec.BrokerAddr == "" || p.BrokerEndpoint == "" {
		return ""
	}
	return " --provider-broker-file " + psQuote(toShellPath(p.BrokerEndpoint))
}

func (windowsShell) Fetch(d releaseasset.CLIDownload, dir, bin string) string {
	return WindowsFetchCommand(d, dir, bin)
}

// Alive reports 1 only when the pid is running and its command line is the
// serve this bootstrap started. Checking the command line — not just that
// something holds the pid — is what stops a recycled pid from being mistaken
// for it and later killed.
func (windowsShell) Alive(pid int, p StatePaths) string {
	return psCommand(strings.Join([]string{
		commandLineOf(pid),
		"if (" + ownsServe(p) + ") { '1' } else { '0' }",
	}, "; "))
}

func (windowsShell) Stop(pid int, p StatePaths) string {
	return psCommand(strings.Join([]string{
		commandLineOf(pid),
		"if (" + ownsServe(p) + ") { Stop-Process -Id " + fmt.Sprint(pid) + " -Force -ErrorAction SilentlyContinue }",
	}, "; "))
}

func (windowsShell) Logs(logFile string, n int) string {
	if n <= 0 {
		n = 200
	}
	path := psQuote(toShellPath(logFile))
	return psCommand("if (Test-Path -LiteralPath " + path + ") { Get-Content -LiteralPath " + path +
		" -Tail " + fmt.Sprint(n) + " -ErrorAction SilentlyContinue }")
}

// Downloader is never empty on Windows: Invoke-WebRequest is part of the shell
// this whole file already requires.
func (windowsShell) Downloader() string { return psCommand("'iwr'") }

func (windowsShell) NPMVersion() string {
	return psCommand("(& npm --version 2>$null | Select-Object -First 1) -join ''")
}

// Locate reports the same records the POSIX probe does, one block per candidate:
// `bin`, `ver`, `flag`. Every candidate for the same reason as there — the
// caller is the side that knows which of them is usable for what it wants.
func (windowsShell) Locate(uploadedBin string, flags []string) string {
	uploaded := psQuote(toShellPath(uploadedBin))
	checks := make([]string, 0, len(flags))
	for _, f := range flags {
		// Anchored the way the POSIX side is: -provider-broker is a prefix of
		// -provider-broker-token-file, and Contains would call the short one
		// present whenever only the long one is.
		checks = append(checks, "if ([regex]::IsMatch($h, "+psQuote("-"+f+"(\\s|$)")+")) { "+
			psQuote("flag "+f+" yes")+" } else { "+psQuote("flag "+f+" no")+" }")
	}
	return psCommand(strings.Join([]string{
		"function probe($p) { if (-not $p -or -not (Test-Path -LiteralPath $p)) { return }; " +
			"'bin ' + $p; " +
			"'ver ' + ((& $p --version 2>$null | Select-Object -First 1) -join ''); " +
			"$h = (& $p serve --help 2>&1 | Out-String); " +
			strings.Join(checks, "; ") + " }",
		"$c = Get-Command 'reasonix.exe' -ErrorAction SilentlyContinue",
		"if ($c) { probe $c.Source }",
		"probe " + uploaded,
	}, "; "))
}

// commandLineOf binds $c to a pid's command line, or nothing when it is gone.
func commandLineOf(pid int) string {
	return fmt.Sprintf("$c = (Get-CimInstance Win32_Process -Filter 'ProcessId=%d' -ErrorAction SilentlyContinue).CommandLine", pid)
}

// ownsServe is the test for "this is the process we started". Contains, not
// -like: a path is data here, and the wildcard operator would read a bracket
// in one as a pattern.
func ownsServe(p StatePaths) string {
	return "$c -and $c.Contains(" + psQuote(toShellPath(p.TokenFile)) +
		") -and $c.Contains(" + psQuote(toShellPath(p.PortFile)) + ")"
}

// psQuote makes a PowerShell string literal. Single quotes suppress every
// expansion the language has, and the only escape inside one is a doubled
// quote — the same shape shellQuote relies on for POSIX.
func psQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// psCommand wraps a script so that cmd, which receives it, sees only base64.
// Progress is silenced first: PowerShell serialises its progress records as
// CLIXML, and a module loading for the first time would otherwise put a
// document where the caller is reading a path.
func psCommand(script string) string {
	return "powershell -NoProfile -NonInteractive -EncodedCommand " +
		encodePS("$ProgressPreference='SilentlyContinue'; "+script)
}

func encodePS(script string) string {
	units := utf16.Encode([]rune(script))
	raw := make([]byte, 0, len(units)*2)
	for _, u := range units {
		raw = append(raw, byte(u), byte(u>>8))
	}
	return base64.StdEncoding.EncodeToString(raw)
}

// toSFTPPath is how the file layer addresses a Windows path: its SFTP server
// answers with /C:/Users/... for what the shell calls C:\Users\...
func toSFTPPath(p string) string {
	p = strings.ReplaceAll(p, `\`, "/")
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return p
}

// NativePath is what that machine's own kernel takes — the shell spelling,
// which is not the one SFTP answers with.
func (windowsShell) NativePath(p string) string { return toShellPath(p) }

// toShellPath is the reverse, for a path going into a script.
func toShellPath(p string) string {
	return strings.ReplaceAll(strings.TrimPrefix(p, "/"), "/", `\`)
}
