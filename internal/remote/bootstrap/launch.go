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
	// BrokerTokenFile authenticates this serve to the provider broker on the
	// machine that started it. Written only when one was configured.
	BrokerTokenFile string
	// BrokerEndpoint is where that broker is and the token it takes, one file
	// rewritten whole when a connect publishes the broker elsewhere.
	BrokerEndpoint string
	LogFile        string
	PortFile       string
	PidFile        string
	LockDir        string
	LockOwner      string
}

// shellQuote wraps s in single quotes safe for POSIX sh, escaping embedded
// single quotes as '\”. This is the only quoting used for remote command
// operands; every interpolated path/workspace passes through it.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// LaunchSpec is what one serve is started with beyond where its state lives.
type LaunchSpec struct {
	Bin       string
	Workspace string
	// BrokerAddr is the remote loopback address an -R forward publishes the
	// starting machine's provider broker on. Empty leaves this serve resolving
	// providers — and needing credentials — of its own.
	BrokerAddr string
}

// LaunchCommand builds the `sh -c` script that starts a detached serve in the
// workspace, writing the port/pid files and appending output to the log. The
// binary path and every operand are single-quote-escaped so hostile paths
// (spaces, quotes, `; rm -rf ~`) cannot break out.
//
// Detachment: `setsid` fully divorces the process from any session, but it is
// absent on stock macOS, so it is used only when present (`$SX`); `nohup` +
// backgrounding + `</dev/null` is sufficient over a non-interactive SSH exec.
// The log is created 0600 (umask 077 + explicit chmod) so a same-machine user
// cannot read serve output; serve is launched with `--port-file`, which
// suppresses its token share line, so the token never reaches the log.
// It echoes the shell's $! so the caller can record the pid immediately.
func LaunchCommand(spec LaunchSpec, p StatePaths) string {
	return fmt.Sprintf(
		"mkdir -p %s && cd %s && rm -f %s %s && umask 077 && : >>%s && chmod 600 %s && "+
			"SX=; command -v setsid >/dev/null 2>&1 && SX=setsid; "+
			"$SX nohup %s serve --addr 127.0.0.1:0 --auth token --token-file %s --port-file %s --pid-file %s%s </dev/null >>%s 2>&1 & echo $!",
		shellQuote(p.Dir),
		shellQuote(spec.Workspace),
		shellQuote(p.PortFile),
		shellQuote(p.PidFile),
		shellQuote(p.LogFile),
		shellQuote(p.LogFile),
		shellQuote(spec.Bin),
		shellQuote(p.TokenFile),
		shellQuote(p.PortFile),
		shellQuote(p.PidFile),
		posixBrokerArgs(spec, p),
		shellQuote(p.LogFile),
	)
}

// posixBrokerArgs is the broker half of the serve command line, empty when
// this launch configured none. The token rides a file for the same reason the
// serve token does: argv is world-readable in `ps`.
func posixBrokerArgs(spec LaunchSpec, p StatePaths) string {
	if spec.BrokerAddr == "" || p.BrokerEndpoint == "" {
		return ""
	}
	return " --provider-broker-file " + shellQuote(p.BrokerEndpoint)
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
// mistaken for the serve and later signalled by StopCommand.
func ServeAliveCommand(pid int, p StatePaths) string {
	return fmt.Sprintf(
		"T=%s; P=%s; kill -0 %d 2>/dev/null || { echo 0; exit 0; }; "+
			"A=$(ps -p %d -o args= 2>/dev/null || ps -p %d -o command= 2>/dev/null); "+
			"case \"$A\" in *reasonix*serve*\"$T\"*\"$P\"*) echo 1;; *) echo 0;; esac",
		shellQuote(p.TokenFile), shellQuote(p.PortFile), pid, pid, pid,
	)
}

// LogsCommand tails n lines of the log file (n<=0 => 200).
func LogsCommand(logFile string, n int) string {
	if n <= 0 {
		n = 200
	}
	return fmt.Sprintf("tail -n %d %s 2>/dev/null || true", n, shellQuote(logFile))
}

// LaunchFlags names the flags LaunchCommand puts on the command line, which is
// exactly what the probe asks a kernel about. One flag cannot stand for the
// set: --port-file predates the broker pair by a release line, so a kernel
// carrying only that one answered the probe yes and then exited on a flag it
// had never defined.
func LaunchFlags(withBroker bool) []string {
	flags := []string{"addr", "auth", "token-file", "port-file", "pid-file"}
	if withBroker {
		flags = append(flags, "provider-broker-file")
	}
	return flags
}

// flagChecks renders one `flag <name> yes|no` test per flag against $H, the
// help text the caller has already captured.
func flagChecks(flags []string) string {
	var b strings.Builder
	for _, f := range flags {
		// -name up to whitespace or the line end: -provider-broker is a prefix
		// of -provider-broker-token-file, and a substring match would report
		// the shorter one present whenever only the longer one is.
		b.WriteString("if printf '%s\\n' \"$H\" | grep -qE -- " + shellQuote("-"+f+"([[:space:]]|$)") +
			"; then echo " + shellQuote("flag "+f+" yes") + "; else echo " + shellQuote("flag "+f+" no") + "; fi; ")
	}
	return b.String()
}

// LocateCommand reports every reasonix it finds as `bin <path>`, `ver <what
// --version said>`, and one `flag <name> yes|no` line per flag the launch will
// pass. Every candidate, not the first path that exists: a machine can hold an
// old reasonix on PATH and a current one this bootstrap uploaded beside it, and
// only the caller knows which of the two counts.
func LocateCommand(uploadedBin string, flags []string) string {
	return fmt.Sprintf(
		"probe() { [ -n \"$1\" ] && [ -x \"$1\" ] || return 0; "+
			"echo \"bin $1\"; echo \"ver $(\"$1\" --version 2>/dev/null | head -n 1)\"; "+
			"H=$(\"$1\" serve --help 2>&1); %s}; "+
			"probe \"$(command -v reasonix 2>/dev/null)\"; "+
			"probe %s; "+
			"P=\"$(npm prefix -g 2>/dev/null)\"; "+
			"if [ -n \"$P\" ]; then probe \"$P/bin/reasonix\"; fi; "+
			"exit 0",
		flagChecks(flags), shellQuote(uploadedBin),
	)
}
