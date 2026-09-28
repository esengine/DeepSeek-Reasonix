//go:build !darwin && !windows

package sandbox

// confinedWriteDirs is every host directory bubblewrap binds writable for spec.
// Host temp and the session temp count as writable too.
func confinedWriteDirs(spec Spec) []string {
	dirs := planWriteRoots(spec, backendWriteDirs(spec)).dirs
	return append(dirs, callerWriteDirs(Spec{SessionTemp: spec.SessionTemp, MinimalWrites: spec.MinimalWrites})...)
}
