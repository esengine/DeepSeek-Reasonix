//go:build !windows

package builtin

// commandLineUnits reports no ceiling; only Windows bounds one command line.
func commandLineUnits([]string) (int, int) { return 0, 0 }
