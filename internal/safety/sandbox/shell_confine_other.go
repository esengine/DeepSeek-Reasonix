//go:build !windows

package sandbox

// BashConfine is nil off Windows: the other backends confine a bash without
// asking it to start under a token.
func BashConfine(string, string) func(string) bool { return nil }
