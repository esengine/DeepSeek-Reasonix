//go:build !darwin

package update

// MaybeRunMacHandoff is a no-op off macOS. Every host's main calls it before
// anything else so a re-executed handoff child never reaches normal startup;
// keeping the stub means no host has to know which platform it is compiled for.
func MaybeRunMacHandoff([]string) (handled bool, exitCode int) { return false, 0 }

// ApplicationAt off macOS has no bundle to name, only the process a staged
// swap must wait out.
func ApplicationAt(_ string, pid int) (Application, error) { return Application{PID: pid}, nil }
