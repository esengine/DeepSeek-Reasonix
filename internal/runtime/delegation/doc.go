// Package delegation runs work on sub-agents: the task tool and its read-only
// variant, fleets and parallel tasks, the durable record of each child run,
// and the profile and capability grant a child is started with. It drives
// agents only through the agent package's exported surface; the executor loop
// knows a tool spawns children only through the interfaces it declares.
package delegation
