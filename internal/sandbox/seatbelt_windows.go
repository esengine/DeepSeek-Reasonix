//go:build windows

package sandbox

// Command returns the native shell invocation unwrapped. Windows currently has
// no Reasonix OS-level Bash sandbox; config.BashModeForGOOS keeps the effective
// product setting fixed to off. Returning wrapped=false also preserves the
// fail-closed contract for any internal caller that constructs an enforce Spec
// directly.
func Command(spec Spec, sh Shell, command string) ([]string, bool) {
	return sh.argv(command), false
}

// CommandArgs is like Command but accepts the command as raw argv instead of a
// shell command string.
func CommandArgs(spec Spec, args []string) ([]string, bool) {
	return args, false
}

// Available reports that Reasonix does not currently ship an OS-level Bash
// sandbox on Windows.
func Available() bool {
	return false
}
