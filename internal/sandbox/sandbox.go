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
	// Network allows network egress from inside the sandbox. Off blocks it so a
	// command cannot exfiltrate or fetch; many dev commands (module/package
	// downloads) need it, so it defaults on at the config layer.
	Network bool
	// MinimalWrites omits the broad build-tool cache write allowances used by
	// the bash sandbox. MCP profiles set it and explicitly provide only their
	// private state/temp directories (plus approved writer roots).
	MinimalWrites bool
	// Shell is the interpreter the bash tool runs under. A zero value (empty
	// Path) means the tool resolves one itself; the composition root sets it from
	// [tools.shell] so the configured choice rides along with the spec.
	Shell Shell
}

// Enforce reports whether the spec asks for confinement.
func (s Spec) Enforce() bool { return s.Mode == "enforce" }

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
		return "Ensure `sandbox-exec` is available on PATH or set [sandbox] bash = \"off\" in config.toml / Settings -> Sandbox to restore pre-1.16 unconfined shell execution."
	case "windows":
		return "Windows does not currently provide a Reasonix OS-level Bash sandbox; the effective setting is fixed to \"off\" and shell commands run unconfined."
	default:
		return "Set [sandbox] bash = \"off\" in config.toml / Settings -> Sandbox to run shell commands unconfined on this platform."
	}
}

// BackendUnavailableReason is safe diagnostic copy for subsystems such as MCP
// that intentionally continue unconfined when the OS backend is missing.
func BackendUnavailableReason() string {
	switch runtime.GOOS {
	case "linux":
		return "bubblewrap (bwrap) is unavailable on PATH"
	case "darwin":
		return "sandbox-exec is unavailable on PATH"
	case "windows":
		return "the AppContainer helper or required Windows sandbox APIs are unavailable"
	default:
		return "this platform has no supported Reasonix sandbox backend"
	}
}
