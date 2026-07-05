// Package sandbox wraps a shell command in an OS-level jail so the model's
// `bash` calls are confined: it may read almost freely but write only inside
// the writable roots (workspace, configured extras, plus temp and toolchain
// caches) , with optional forbid-read roots, and reach the network only when
// allowed. This is the *enforcement* layer beneath the permission rules
// (*policy*): a permitted command still cannot escape the box.
//
// macOS uses Seatbelt via sandbox-exec, Linux uses bubblewrap when available,
// and platforms without OS tooling fall back to running the command unwrapped
// (see Available). Confining the in-process file-writer built-ins is handled
// separately, in package tool/builtin.
package sandbox

// Spec describes how to confine one command. The zero value (Mode == "") does
// not enforce, so an unconfigured caller runs commands unchanged.
type Spec struct {
	// Mode is "enforce" to wrap the command, anything else (incl. "off" and "")
	// to run it unwrapped.
	Mode string
	// WriteRoots are directories the command may write to (the workspace root
	// plus any configured extras). Temp dirs and common toolchain caches are
	// added automatically so builds and package managers keep working.
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
