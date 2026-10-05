// Package agentgraph is the vocabulary of one run's execution graph: the nodes
// a turn handed work to, and the typed edges between them.
//
// It exists because that structure was being re-derived in five places and
// survived in none. A fleet's dependency DAG died with the call that built it,
// the persisted sidecar recorded a parent link and nothing else, and the event
// stream implied edges through id prefixes. A reader asking why a node waited
// could not tell an unmet dependency from a delivered answer, because neither
// had ever been written down as anything a reader could tell apart.
//
// Nothing here knows what an agent, a tool, or a provider is. A producer
// publishes a Delta of what it has just proven; every consumer folds deltas
// with the same Apply, so the graph a headless run reports is the graph the
// desktop draws.
package agentgraph
