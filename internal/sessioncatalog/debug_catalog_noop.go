//go:build !debugpprof

package sessioncatalog

// debugPhase and debugSkipped are no-ops unless the debugpprof tag is set.
func debugPhase(string, string) func() { return func() {} }

func debugSkipped(string) {}
