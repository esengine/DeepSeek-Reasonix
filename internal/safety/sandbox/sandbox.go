// Package sandbox confines processes using macOS Seatbelt, Linux bubblewrap,
// or optional Windows creation grants. Windows grants do not bound deletion
// and rename. Execution requirements distinguish these protection levels;
// unsupported complete mutation boundaries refuse before process launch.
// In-process file tools enforce their access separately.
package sandbox

import (
	"errors"
	"os"
	"runtime"
)

// Spec describes how to confine one command. The zero value (Mode == "") does
// not enforce, so an unconfigured caller runs commands unchanged.
type Spec struct {
	RequiredIntegrity   IntegrityRequirement
	ProtectedWriteRoots []string
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
	// ReadRoots are explicit host read grants. Linux restores declared temporary
	// directories hidden by private /tmp; Windows uses them for AppContainer reads.
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
	// AllowDevices are globs under /dev for host device nodes bound into the
	// Linux sandbox's private /dev (the GPU, for one). Other platforms ignore it.
	AllowDevices []string
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
	// GrantSID is the synthetic SID the Windows launcher restricts a command's
	// token to: the one the GrantBook granted on WriteRoots. Empty leaves a
	// Windows command unwrapped, which a caller in enforce mode treats as refused.
	GrantSID string
	// GrantFault is why GrantSpec could not prepare the Windows sandbox, as one of
	// the ErrWin* identities; nil when it did, or when nothing was asked for.
	GrantFault error
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
func UnavailableMessage() string { return unavailableMessage(bwrapFault()) }

// UnavailableMessageFor is UnavailableMessage for the launch spec will make.
func UnavailableMessageFor(spec Spec) string {
	if err := RequiredIntegrityFault(spec); err != nil {
		return err.Error()
	}
	return unavailableMessage(bwrapFaultFor(spec))
}

func unavailableMessage(fault error) string {
	return "bash sandbox requested but unavailable on this host; refusing to run unconfined. " + remediation(fault)
}

// UnavailableRemediation is split out so status surfaces can append the same
// actionable hint without repeating the leading error.
func UnavailableRemediation() string { return remediation(bwrapFault()) }

// UnavailableRemediationFor is UnavailableRemediation for the launch spec will make.
func UnavailableRemediationFor(spec Spec) string { return remediation(bwrapFaultFor(spec)) }

func remediation(fault error) string {
	switch runtime.GOOS {
	case "linux":
		if errors.Is(fault, ErrBwrapNetworkDenied) {
			return "Bubblewrap can start here but cannot give a command its own network namespace, which needs CAP_NET_ADMIN (a default container lacks it). " +
				"Grant the container CAP_NET_ADMIN, or set [sandbox] network = true so commands share the host network, " +
				"or set [sandbox] bash = \"off\" in config.toml / Settings -> Sandbox; shell commands then run unconfined and the container or host is the only boundary."
		}
		if errors.Is(fault, ErrBwrapNamespaceDenied) {
			return "Bubblewrap is installed but cannot create a namespace here, which is typical inside a default container or on a kernel that restricts unprivileged user namespaces. " +
				"Run the container with the privileges bubblewrap needs (typically CAP_SYS_ADMIN together with an unconfined seccomp profile; either widens the container itself), " +
				"or set [sandbox] bash = \"off\" in config.toml / Settings -> Sandbox; shell commands then run unconfined and the container or host is the only boundary."
		}
		if errors.Is(fault, ErrBwrapUnrunnable) {
			return "Bubblewrap is on PATH but could not be started (it may not be executable here, or did not finish its check in time). Check that `bwrap --version` runs, " +
				"or set [sandbox] bash = \"off\" in config.toml / Settings -> Sandbox; shell commands then run unconfined and the container or host is the only boundary."
		}
		return "Install bubblewrap (`bwrap`) or set [sandbox] bash = \"off\" in config.toml / Settings -> Sandbox to restore pre-1.16 unconfined shell execution."
	case "darwin":
		return "Ensure `sandbox-exec` is installed and usable (the host must allow `sandbox_apply`), or set [sandbox] bash = \"off\" in config.toml / Settings -> Sandbox to restore pre-1.16 unconfined shell execution."
	case "windows":
		return windowsRemediation(fault)
	default:
		return "Set [sandbox] bash = \"off\" in config.toml / Settings -> Sandbox to run shell commands unconfined on this platform."
	}
}

// ErrBwrapMissing, ErrBwrapNamespaceDenied, ErrBwrapNetworkDenied and
// ErrBwrapUnrunnable are why a Linux host has no usable bash sandbox: no
// bubblewrap on PATH, one that cannot enter a namespace, one that enters it but
// not a network namespace, or one that could not be started at all.
var (
	ErrBwrapMissing         = errors.New("bubblewrap is not installed")
	ErrBwrapNamespaceDenied = errors.New("bubblewrap cannot create a namespace here")
	ErrBwrapNetworkDenied   = errors.New("bubblewrap cannot create a network namespace here")
	ErrBwrapUnrunnable      = errors.New("bubblewrap could not be started")
)

// UnavailableReason is the identity behind the unavailable state, for a caller
// that branches on it with errors.Is. Nil when the sandbox is usable or the
// platform names no finer reason than its UnavailableCode.
func UnavailableReason() error { return bwrapFault() }

// AvailableFor reports whether a command launched for spec can be confined: a
// spec that denies network needs a network namespace that Available alone does
// not prove.
func AvailableFor(spec Spec) bool {
	return RequiredIntegrityFault(spec) == nil && Available() && bwrapFaultFor(spec) == nil
}

// bwrapFault is the base fault, with the network left open.
func bwrapFault() error { return bwrapFaultFor(Spec{Network: true}) }

// UnavailableCodeFor is that explanation's identity. A frontend says it in the
// reader's language from this; matching the English sentence above would be a
// second copy of this switch, in a place that cannot see when it changes.
func UnavailableCodeFor(spec Spec) string {
	if RequiredIntegrityFault(spec) != nil {
		return "sandbox.incomplete_integrity"
	}
	return unavailableCode(bwrapFaultFor(spec))
}

func unavailableCode(fault error) string {
	switch runtime.GOOS {
	case "linux":
		if errors.Is(fault, ErrBwrapNetworkDenied) {
			return "sandbox.bwrap_network_denied"
		}
		if errors.Is(fault, ErrBwrapNamespaceDenied) {
			return "sandbox.bwrap_namespace_denied"
		}
		if errors.Is(fault, ErrBwrapUnrunnable) {
			return "sandbox.bwrap_unrunnable"
		}
		return "sandbox.no_bubblewrap"
	case "darwin":
		return "sandbox.no_sandbox_exec"
	case "windows":
		return windowsUnavailableCode(fault)
	default:
		return "sandbox.unsupported_platform"
	}
}
