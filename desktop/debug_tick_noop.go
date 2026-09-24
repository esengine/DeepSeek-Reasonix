//go:build !debugpprof

package main

// debugTickPhase and debugNote are no-ops unless the debugpprof tag is set.
func debugTickPhase(string) func() { return func() {} }

func debugNote(string, ...any) {}
