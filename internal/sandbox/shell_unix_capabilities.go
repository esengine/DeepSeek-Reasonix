package sandbox

// unixShellCapabilities reports the common POSIX-family interpreters present
// on macOS and Linux. Linux remains Bash-only for execution; macOS may fall
// back to zsh and then sh when Bash is unavailable, while Settings reports the
// complete inventory on both platforms.
func unixShellCapabilities(snap *shellSnapshot) []ShellCapability {
	return []ShellCapability{
		unixShellCapability(snap, ShellCapabilityBash, []string{"bash"}, []string{"/bin/bash", "/usr/bin/bash"}),
		unixShellCapability(snap, ShellCapabilityZsh, []string{"zsh"}, []string{"/bin/zsh", "/usr/bin/zsh"}),
		unixShellCapability(snap, ShellCapabilitySh, []string{"sh"}, []string{"/bin/sh", "/usr/bin/sh"}),
	}
}

func unixShellCapability(snap *shellSnapshot, id string, names, standardPaths []string) ShellCapability {
	capability := ShellCapability{ID: id, Variant: "system"}
	for _, name := range names {
		if path, err := snap.lookPath(name); err == nil {
			capability.Available = true
			capability.Path = path
			capability.Source = ShellSourcePath
			return capability
		}
	}
	for _, path := range standardPaths {
		if snap.exists(path) {
			capability.Available = true
			capability.Path = path
			capability.Source = ShellSourceStandard
			return capability
		}
	}
	capability.Reason = "not-found"
	return capability
}
