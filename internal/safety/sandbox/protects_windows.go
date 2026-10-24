package sandbox

// The Windows directory plan describes creation grants, not complete mutation
// protection: the token does not bound deletion and rename.
func confinedWriteDirs(spec Spec) []string { return callerWriteDirs(spec) }
