// Package sandbox wraps a shell command in an OS-level jail so the model's
// `bash` calls are confined: it may read almost freely but write only inside
// the writable roots (workspace, configured extras, plus temp and toolchain
// caches), with optional forbid-read roots, and reach the network only when
// allowed. This is the *enforcement* layer beneath the permission rules
// (*policy*): a permitted command still cannot escape the box.
//
// macOS uses Seatbelt via sandbox-exec, Linux uses bubblewrap when available,
// and Windows uses a helper process backed by github.com/SivanCola/windows-sandbox:
// AppContainer for read-only commands, a low-integrity token for writable
// commands, and a kill-on-close Job Object. When enforce is requested but no
// OS sandbox backend is available, the bash tool fails closed instead of
// running the command unwrapped. Confining the in-process file-writer
// built-ins is handled separately, in package tool/builtin.
package sandbox

import (
	"crypto/sha256"
	"encoding/hex"
	"runtime"
)

// WindowsHelperCommand is an internal CLI subcommand used only by the Windows
// sandbox wrapper. It is intentionally obscure so it does not collide with
// public commands.
const WindowsHelperCommand = "__reasonix_windows_sandbox"

const windowsSandboxFailureMarkerPrefix = "__reasonix_windows_sandbox_failure__:"

// WindowsSandboxFailureMarker returns the helper-only marker printed when the
// native Windows sandbox backend fails before starting the child command.
func WindowsSandboxFailureMarker(payload string) string {
	sum := sha256.Sum256([]byte(payload))
	return windowsSandboxFailureMarkerPrefix + hex.EncodeToString(sum[:])
}

// WindowsSandboxFailureMarkerFromCommand extracts the marker expected from a
// Windows sandbox helper argv produced by Command/CommandArgs.
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
	// WriteRoots are directories the command may write to (the workspace root
	// plus any configured extras). Platforms may add command-scoped temp/cache
	// roots so builds and package managers keep working without broad writes.
	WriteRoots []string
	// ForbidReadRoots are directories the command may not read from when
	// confined. The OS sandbox denies access to these paths (macOS Seatbelt
	// deny file-read* rules, Linux bubblewrap --tmpfs overlays); on other
	// platforms the in-process tools enforce this instead.
	ForbidReadRoots []string
	// Network allows network egress from inside the sandbox. Off blocks it so a
	// command cannot exfiltrate or fetch; many dev commands (module/package
	// downloads) need it, so it defaults on at the config layer.
	Network bool
	// Shell is the interpreter the bash tool runs under. A zero value (empty
	// Path) means the tool resolves one itself; the composition root sets it from
	// [tools.shell] so the configured choice rides along with the spec.
	Shell Shell
}

// Enforce reports whether the spec asks for confinement.
func (s Spec) Enforce() bool { return s.Mode == "enforce" }

// UnavailableMessage explains why an enforced bash sandbox cannot run and gives
// the user the two durable fixes: install an OS sandbox backend, or opt into the
// older unconfined behavior explicitly.
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
		return "The native Windows sandbox backend (AppContainer) is unavailable on this host; set [sandbox] bash = \"off\" in config.toml / Settings -> Sandbox to run shell commands unconfined."
	default:
		return "Set [sandbox] bash = \"off\" in config.toml / Settings -> Sandbox to run shell commands unconfined on this platform."
	}
}
