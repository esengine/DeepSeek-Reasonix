// tool_facts.go — what a receipt is told about the tool behind a call.
package evidence

// ToolFacts is what a receipt needs to know about the tool behind a call,
// asked of the tool rather than guessed from its name. The host fills it from
// the live registry, and a replay looks the same name up the same way.
type ToolFacts struct {
	ReadOnly bool
	// WritesNamedPaths marks a tool whose effect is writing the paths its
	// arguments name, which is what makes the write attributable to a file.
	WritesNamedPaths bool
	// EffectsUnstated marks a tool acting through a program the host does not
	// see into, whose call is a mutation only if the workspace shows one.
	EffectsUnstated bool
}
