package bootstrap

import (
	"fmt"
	"strings"
)

// StatePaths are the absolute remote-side paths for one workspace's serve
// state. All are under ~/.reasonix/remote.
type StatePaths struct {
	Dir       string // ~/.reasonix/remote
	StateJSON string
	TokenFile string
	LogFile   string
	PortFile  string
	PidFile   string
	LockDir   string
	LockOwner string
}

// shellQuote wraps s in single quotes safe for POSIX sh, escaping embedded
// single quotes as '\”. This is the only quoting used for remote command
// operands; every interpolated path/workspace passes through it.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// LaunchCommand starts a detached serve with shell-quoted operands, 0600 log,
// and file-based port, pid, and auth token state. It uses setsid when present
// and falls back to nohup on stock macOS. Credential-proxy mode selects the
// tunnel-backed provider; its scoped token remains in the remote 0600 .env
// and never appears in this command.
func LaunchCommand(bin, workspace string, p StatePaths, cred *CredentialProxyOptions) string {
	modelFlag := ""
	if cred != nil {
		modelFlag = " --model " + shellQuote(cred.Provider)
	}
	return fmt.Sprintf(
		"mkdir -p %s && cd %s && rm -f %s %s && umask 077 && : >>%s && chmod 600 %s && "+
			"SX=; command -v setsid >/dev/null 2>&1 && SX=setsid; "+
			"$SX nohup %s serve --addr 127.0.0.1:0 --auth token --token-file %s --port-file %s --pid-file %s%s </dev/null >>%s 2>&1 & echo $!",
		shellQuote(p.Dir),
		shellQuote(workspace),
		shellQuote(p.PortFile),
		shellQuote(p.PidFile),
		shellQuote(p.LogFile),
		shellQuote(p.LogFile),
		shellQuote(bin),
		shellQuote(p.TokenFile),
		shellQuote(p.PortFile),
		shellQuote(p.PidFile),
		modelFlag,
		shellQuote(p.LogFile),
	)
}

// StopCommand builds a script that TERMs the pid, waits up to ~5s, then KILLs
// if still alive. pid is validated numeric by the caller, and the caller has
// already confirmed (ServeAliveCommand) that the pid is our serve, so PID reuse
// cannot cause an unrelated process to be signalled.
func StopCommand(pid int, p StatePaths) string {
	return fmt.Sprintf(
		"T=%s; P=%s; ours() { A=$(ps -p %d -o args= 2>/dev/null || ps -p %d -o command= 2>/dev/null); "+
			"case \"$A\" in *reasonix*serve*\"$T\"*\"$P\"*) return 0;; *) return 1;; esac; }; "+
			"ours || exit 0; kill -TERM %d 2>/dev/null; "+
			"for i in 1 2 3 4 5; do kill -0 %d 2>/dev/null || exit 0; ours || exit 0; sleep 1; done; "+
			"ours && kill -KILL %d 2>/dev/null; exit 0",
		shellQuote(p.TokenFile), shellQuote(p.PortFile), pid, pid, pid, pid, pid,
	)
}

// ServeAliveCommand prints "1" only when pid is running AND its command line
// looks like a reasonix serve process. Checking the args (not just `kill -0`)
// prevents a recycled PID — now owned by an unrelated process — from being
// mistaken for the serve and later signalled by StopCommand. Each requireArgs
// fragment must additionally appear in the args, in order after the token and
// port files: local-proxy mode requires "--model <proxy provider>" so a serve
// launched under different settings (e.g. before the host switched credential
// modes) is not treated as reusable.
func ServeAliveCommand(pid int, p StatePaths, requireArgs ...string) string {
	var decls strings.Builder
	fmt.Fprintf(&decls, "T=%s; P=%s; ", shellQuote(p.TokenFile), shellQuote(p.PortFile))
	var pattern strings.Builder
	pattern.WriteString("*reasonix*serve*\"$T\"*\"$P\"*")
	for i, arg := range requireArgs {
		fmt.Fprintf(&decls, "R%d=%s; ", i, shellQuote(arg))
		fmt.Fprintf(&pattern, "\"$R%d\"*", i)
	}
	return fmt.Sprintf(
		"%skill -0 %d 2>/dev/null || { echo 0; exit 0; }; "+
			"A=$(ps -p %d -o args= 2>/dev/null || ps -p %d -o command= 2>/dev/null); "+
			"case \"$A\" in %s) echo 1;; *) echo 0;; esac",
		decls.String(), pid, pid, pid, pattern.String(),
	)
}

// LogsCommand tails n lines of the log file (n<=0 => 200).
func LogsCommand(logFile string, n int) string {
	if n <= 0 {
		n = 200
	}
	return fmt.Sprintf("tail -n %d %s 2>/dev/null || true", n, shellQuote(logFile))
}

// servePortFileMarker is what LocateCommand greps for in `serve --help` to
// decide the located binary supports --port-file/--token-file. It must match
// the flag name registered in runServe.
const servePortFileMarker = "port-file"

// LocateCommand probes for a usable reasonix binary. It prints three lines:
// the resolved path (or empty), the `--version` output, and "portfile:yes" when
// `serve --help` advertises the --port-file flag. The bootstrap gates on the
// flag, not the version number, because --port-file/--token-file ship in this
// change: a version gate cannot know its own future release number, and any
// already-released binary would pass a numeric gate yet still lack the flags.
func LocateCommand(uploadedBin string) string {
	return fmt.Sprintf(
		"BIN=\"$(command -v reasonix 2>/dev/null)\"; "+
			"if [ -z \"$BIN\" ] && [ -x %s ]; then BIN=%s; fi; "+
			"if [ -z \"$BIN\" ]; then P=\"$(npm prefix -g 2>/dev/null)\"; if [ -n \"$P\" ] && [ -x \"$P/bin/reasonix\" ]; then BIN=\"$P/bin/reasonix\"; fi; fi; "+
			"echo \"$BIN\"; "+
			"if [ -n \"$BIN\" ]; then \"$BIN\" --version 2>/dev/null; "+
			"if \"$BIN\" serve --help 2>&1 | grep -q -- %s; then echo portfile:yes; else echo portfile:no; fi; fi",
		shellQuote(uploadedBin), shellQuote(uploadedBin), shellQuote(servePortFileMarker),
	)
}
