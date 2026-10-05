package sandbox

// confinedWriteDirs is unused on Windows, where Available reports false and no
// backend confines a shell command's writes.
func confinedWriteDirs(spec Spec) []string { return callerWriteDirs(spec) }
