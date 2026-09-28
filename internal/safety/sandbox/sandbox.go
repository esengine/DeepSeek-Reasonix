// Package sandbox wraps a shell command in an OS-level jail so the model's
// `bash` calls are confined: it may read almost freely but write only inside
// the writable roots (workspace, configured extras, plus temp and toolchain
// caches), with optional forbid-read roots, and reach the network only when
// allowed. This is the *enforcement* layer beneath the permission rules
// (*policy*): a permitted command still cannot escape the box.
//
// macOS uses Seatbelt via sandbox-exec and Linux uses bubblewrap when available.
// Windows does not currently provide an OS-level bash sandbox and resolves the
// product setting to off. When enforce is requested but no OS sandbox backend
// is available, the bash tool fails closed instead of running the command
// unwrapped.
// Confining the in-process file-writer built-ins is handled separately, in
// package tool/builtin.
package sandbox

import (
	"os"
	"runtime"
)

// Spec describes how to confine one command. The zero value (Mode == "") does
// not enforce, so an unconfigured caller runs commands unchanged.
type Spec struct {
	// Mode is "enforce" to wrap the command, anything else (incl. "off" and "")
	// to run it unwrapped.
	Mode string
	// WriteRoots are directories the command may write to (the workspace root
	// plus any configured extras). Platforms may add command-scoped temp/cache
	// roots so builds and package managers keep working without broad writes.
	WriteRoots []string
	// Pins are the identities WriteRoots had when the session established
	// them; a root that no longer has its identity is not made writable.
	Pins *WriteRootPins
	// ReadRoots are explicit host paths a Windows AppContainer may read. The
	// macOS/Linux profiles already mount the host read-only by default.
	ReadRoots []string
	// AppContainerWriteRoots are the small subset of WriteRoots that a
	// read-only Windows AppContainer may write (for MCP this is only its
	// private state/temp tree). macOS and Linux already enforce this through
	// WriteRoots and ignore this platform-specific distinction.
	AppContainerWriteRoots []string
	// DirectWrites marks a raw-argv launch as a write-capable command. On
	// Windows this selects the low-integrity writer lane; it is deliberately
	// false for ordinary read-only helpers such as rg.
	DirectWrites bool
	// ForbidReadRoots are files or directories the command may not read from
	// when confined. The OS sandbox denies access to these paths (macOS Seatbelt
	// deny file-read* rules, Linux bubblewrap masks); on other platforms the
	// in-process tools enforce this instead.
	ForbidReadRoots []string
	// Network allows *external* egress only; module downloads need it, so it
	// defaults on at the config layer. Host services are HostAuthorities:
	// transport and authority are separate questions, one flag cannot answer both.
	Network bool
	// HostAuthorities are the host services this command may call.
	HostAuthorities []HostAuthority
	// Egress, set with Network where EgressSupported, confines external egress
	// to the host egress proxy. Loopback itself stays open so a command can
	// serve and reach its own listeners.
	Egress EgressRoute
	// ClosedLoopbackPorts stay shut while loopback is open: a host proxy
	// listening there would carry traffic around the egress proxy.
	ClosedLoopbackPorts []int
	// MinimalWrites omits the broad build-tool cache write allowances used by
	// the bash sandbox. MCP profiles set it and explicitly provide only their
	// private state/temp directories (plus approved writer roots).
	MinimalWrites bool
	// Shell is the interpreter the bash tool runs under. A zero value (empty
	// Path) means the tool resolves one itself; the composition root sets it from
	// [tools.shell] so the configured choice rides along with the spec.
	Shell Shell
	// ShellEnv are extra environment variables every bash command inherits, from
	// [tools.shell] env; host-owned session/egress overrides still win.
	ShellEnv map[string]string
	// SessionTemp is the absolute path of the logical-session private temporary
	// directory for this command. When set, Linux bubblewrap binds it at /tmp
	// (instead of a fresh tmpfs), and all platforms export TMPDIR/TMP/TEMP so
	// consecutive Bash calls in the same session share temporary files. Empty
	// keeps the platform default (ephemeral tmpfs on Linux bwrap, host temp
	// elsewhere). MCP and other independent sandboxes leave this empty.
	SessionTemp string
}

// EgressRoute is the host proxy a confined command's external traffic goes
// through. A command presents its token so refusals are charged to it; Ask
// lets a host outside the list be put to a person, and Refusals ends the token.
type EgressRoute interface {
	Port() int
	// SocketPath is the proxy's Unix socket, where EgressNeedsSocket; "" when
	// it has none, which leaves egress shut rather than open.
	SocketPath() string
	Env(token string) []string
	Ask(token string, ask func(host string) (bool, error))
	Refusals(token string) []string
}

// Enforce reports whether the spec asks for confinement.
func (s Spec) Enforce() bool { return s.Mode == "enforce" }

// EffectiveGetenv resolves a variable the way the confined command will see it:
// the host environment with [tools.shell] env applied over it. A client inside
// that command reads the overridden value, so the socket it would connect to is
// the one this resolution has to name.
func EffectiveGetenv(shellEnv map[string]string) func(string) string {
	if len(shellEnv) == 0 {
		return os.Getenv
	}
	return func(key string) string {
		if v, ok := shellEnv[key]; ok {
			return v
		}
		return os.Getenv(key)
	}
}

// UnavailableMessage explains why an enforced bash sandbox cannot run and gives
// the platform-specific remediation.
func UnavailableMessage() string {
	return "bash sandbox requested but unavailable on this host; refusing to run unconfined. " + UnavailableRemediation()
}

// UnavailableRemediation is split out so status surfaces can append the same
// actionable hint without repeating the leading error.
func UnavailableRemediation() string {
	switch runtime.GOOS {
	case "linux":
		return "Install bubblewrap (`bwrap`) or set [sandbox] bash = \"off\" in config.toml / Settings -> Sandbox to restore pre-1.16 unconfined shell execution."
	case "darwin":
		return "Ensure `sandbox-exec` is installed and usable (the host must allow `sandbox_apply`), or set [sandbox] bash = \"off\" in config.toml / Settings -> Sandbox to restore pre-1.16 unconfined shell execution."
	case "windows":
		return "Windows does not currently provide a Reasonix OS-level Bash sandbox; the effective setting is fixed to \"off\" and shell commands run unconfined."
	default:
		return "Set [sandbox] bash = \"off\" in config.toml / Settings -> Sandbox to run shell commands unconfined on this platform."
	}
}

// UnavailableCode is that explanation's identity. A frontend says it in the
// reader's language from this; matching the English sentence above would be a
// second copy of this switch, in a place that cannot see when it changes.
func UnavailableCode() string {
	switch runtime.GOOS {
	case "linux":
		return "sandbox.no_bubblewrap"
	case "darwin":
		return "sandbox.no_sandbox_exec"
	case "windows":
		return "sandbox.unsupported_on_windows"
	default:
		return "sandbox.unsupported_platform"
	}
}
