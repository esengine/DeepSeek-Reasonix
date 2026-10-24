// Package mutation admits complete resource sets for short write transactions.
// It coordinates cooperating processes; it does not grant filesystem access,
// execute tools, or provide a sandbox. Callers authorize the complete effect
// before acquisition, recheck their own preconditions, perform the mutation,
// and end the lease after physical execution settles.
//
// Coordinators that can touch the same files must share a trusted state
// directory. The registry serializes admission, not execution. Live OS locks
// distinguish a queued or active transaction from a dead owner. Compatibility
// claims retain exclusion against older workspacelease writers.
package mutation
