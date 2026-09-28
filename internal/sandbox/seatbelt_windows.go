//go:build windows

package sandbox

// Windows has no OS-level shell sandbox (see OSSandboxSupported), so every
// launch runs unwrapped as the current OS user.

// Command returns the shell invocation unwrapped.
func Command(_ Spec, sh Shell, command string) ([]string, bool) {
	return sh.argv(command), false
}

// CommandArgs returns the raw argv unwrapped.
func CommandArgs(_ Spec, args []string) ([]string, bool) {
	return args, false
}

// Available is always false on Windows.
func Available() bool { return false }

// writableDirsForSpec is empty: no Windows backend confines writes, so there
// is no boundary for Git metadata protection to sit inside.
func writableDirsForSpec(Spec) []string { return nil }

func gitMetadataRoots(spec Spec) []string { return spec.WriteRoots }

// HostWritableDirs is empty: Windows runs commands unjailed, so nothing here
// narrows where a command may write.
func HostWritableDirs() []string { return nil }
