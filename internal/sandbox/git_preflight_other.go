//go:build !darwin

package sandbox

func gitCandidatePreflight(string) bool { return true }
