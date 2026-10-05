// Package coordinator runs a two-model turn: a planner researches read-only
// in its own session and hands a structured plan to an executor agent, which
// carries it out. It drives the executor only through agent's exported
// surface; the agent does not know it is being coordinated beyond the
// handoff role a coordinator marks on it.
package coordinator
