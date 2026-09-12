// Package sandbox wraps a shell command in an OS-level jail so the model's
// `bash` calls are confined: it may read almost freely but write only inside
// the writable roots (workspace, configured extras, plus temp and toolchain
// caches), with optional forbid-read roots, and reach the network only when
// allowed. This is the *enforcement* layer beneath the permission rules
// (*policy*): a permitted command still cannot escape the box.
//
// macOS uses Seatbelt via sandbox-exec and Linux uses bubblewrap when available.
// Windows uses the bundled restricted-token/AppContainer helper and Job Object.
// When an OS sandbox backend is unavailable, restricted presets fail closed
// instead of running the command unwrapped.
// Confining the in-process file-writer built-ins is handled separately, in
// package tool/builtin.
package sandbox

import (
	"crypto/sha256"
	"encoding/hex"
	"runtime"
	"sync/atomic"
	"time"
)

// WindowsHelperCommand is the private subprocess entry point for the native
// Windows sandbox. Host binaries must register and dispatch it before normal
// startup so an enforced launch can never fall through into an unconfined GUI
// or CLI process.
const WindowsHelperCommand = "__reasonix_windows_sandbox"

var helperDispatchRegistered atomic.Bool

func RegisterHelperDispatch() { helperDispatchRegistered.Store(true) }

const windowsSandboxFailureMarkerPrefix = "__reasonix_windows_sandbox_failure__:"

func WindowsSandboxFailureMarker(payload string) string {
	sum := sha256.Sum256([]byte(payload))
	return windowsSandboxFailureMarkerPrefix + hex.EncodeToString(sum[:])
}

func WindowsSandboxFailureMarkerFromCommand(argv []string) (string, bool) {
	if len(argv) < 4 || argv[1] != WindowsHelperCommand || argv[2] == "" || argv[3] != "--" {
		return "", false
	}
	return WindowsSandboxFailureMarker(argv[2]), true
}

// Spec describes how to confine one command. The zero value (Mode == "") does
// not enforce, so an unconfigured caller runs commands unchanged.
type Spec struct {
	// Mode is "enforce" to wrap the command, anything else (incl. "off" and "")
	// to run it unwrapped.
	Mode string
	// ReadOnly removes every ordinary writable mount/allowance. It is distinct
	// from an empty WriteRoots slice, whose historical meaning is unconfigured.
	ReadOnly bool
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
	// Windows this selects the WRITE_RESTRICTED writer lane; it is deliberately
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
	// SessionTemp is the absolute path of the logical-session private temporary
	// directory for this command. When set, Linux bubblewrap binds it at /tmp
	// (instead of a fresh tmpfs), and all platforms export TMPDIR/TMP/TEMP so
	// consecutive Bash calls in the same session share temporary files. Empty
	// keeps the platform default (ephemeral tmpfs on Linux bwrap, host temp
	// elsewhere). MCP and other independent sandboxes leave this empty.
	SessionTemp string
	// ProtectedWriteRoots are Reasonix session/state paths that stay read-only
	// even when a broader WriteRoot such as the user's home directory would
	// otherwise cover them.
	ProtectedWriteRoots []string
	// WindowsLockWait bounds native ACL coordination. A short foreground
	// default avoids hanging an approval; background launches may opt into a
	// larger value. Other platforms ignore it.
	WindowsLockWait time.Duration
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
		return "Install bubblewrap (`bwrap`), or explicitly select Full access for an unconfined session."
	case "darwin":
		return "Ensure `sandbox-exec` is installed and usable (the host must allow `sandbox_apply`), or explicitly select Full access for an unconfined session."
	case "windows":
		return "The native Windows restricted-token/AppContainer sandbox is unavailable. Restricted permission modes refuse to run unconfined; explicitly select Full access only when unconfined execution is intended."
	default:
		return "Restricted permission presets are unavailable on this platform; explicitly select Full access only when unconfined execution is intended."
	}
}

// BackendUnavailableReason is safe diagnostic copy for subsystems such as MCP
// that intentionally continue unconfined when the OS backend is missing.
func BackendUnavailableReason() string {
	switch runtime.GOOS {
	case "linux":
		return "bubblewrap (bwrap) is unavailable on PATH"
	case "darwin":
		return "sandbox-exec is missing from PATH or unusable (sandbox_apply is restricted)"
	case "windows":
		return "the AppContainer helper or required Windows sandbox APIs are unavailable"
	default:
		return "this platform has no supported Reasonix sandbox backend"
	}
}
