//go:build !darwin

package sandbox

import (
	"fmt"
	"os"
	"os/exec"
)

// Command runs the command unwrapped: no OS sandbox is implemented for this
// platform yet (Linux bubblewrap/landlock is the next step). The permission
// layer still gates the call.
//
// When spec.Mode is "enforce" and bubblewrap (bwrap) is available on PATH,
// the command is wrapped in a bubblewrap sandbox with a profile analogous to
// macOS Seatbelt: writes confined to WriteRoots, network denied unless
// spec.Network is true. When bwrap is unavailable, a one-time warning is
// printed to stderr and the command runs unconfined.
func Command(spec Spec, shell, command string) ([]string, bool) {
	if !spec.enforce() {
		return []string{shell, "-c", command}, false
	}
	if bwrap, err := exec.LookPath("bwrap"); err == nil {
		argv := append([]string{bwrap}, bwrapArgs(spec, shell, command)...)
		return argv, true
	}
	// Log a one-time warning when enforce is requested but unavailable.
	fmt.Fprintf(os.Stderr, "sandbox: mode=enforce but no OS sandbox available on this platform; running unconfined\n")
	return []string{shell, "-c", command}, false
}

// Available reports whether an OS sandbox is available on this platform.
// On Linux, this checks for bubblewrap (bwrap) on PATH.
func Available() bool {
	_, err := exec.LookPath("bwrap")
	return err == nil
}

// bwrapArgs builds the bubblewrap command-line arguments that confine the
// shell command to the write roots, deny network unless allowed, and allow
// read access to the whole filesystem (matching the macOS Seatbelt profile's
// read-open policy).
func bwrapArgs(spec Spec, shell, command string) []string {
	args := []string{
		"--unshare-net", // deny network by default
		"--ro-bind", "/", "/",
		"--dev", "/dev",
		"--proc", "/proc",
		"--tmpfs", "/tmp",
	}
	if spec.Network {
		// Re-allow network by removing the network namespace.
		args = args[1:] // drop --unshare-net
	}
	for _, root := range spec.WriteRoots {
		args = append(args, "--bind", root, root)
	}
	args = append(args, shell, "-c", command)
	return args
}

// os is needed for the stderr import on non-darwin platforms.
