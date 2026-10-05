package sandbox

// confinedWriteDirs is every directory the Seatbelt profile for spec allows
// writes to.
func confinedWriteDirs(spec Spec) []string { return writeAllowDirsForSpec(spec) }
