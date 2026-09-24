//go:build !debugpprof

package hostrpc

// debugRPCTiming is a no-op unless the debugpprof tag is set.
func debugRPCTiming(string) func() { return func() {} }
