// Package execgraph rebuilds a run graph from durable facts.
//
// The graph a process draws lives in its event stream and dies with it. What
// outlives the process is the execution journal — how each delegation entered
// orchestration, waited, started and was released — and the sub-agent store's
// record of what happened to the children that ran. This package folds those
// two, plus the set of executions this process still owns, into the same
// vocabulary the live graph is drawn in.
//
// It recomputes rather than restores. Nothing here reads a file, emits an
// event, or knows what an agent or a provider is: the inputs are read models
// the caller has already loaded, and the output is a value.
//
// Two states are deliberately not reconstructed. An execution that was running
// or waiting when its owner disappeared is not put back as running or queued —
// the owner is gone, and a graph that showed either would be describing work
// nobody is doing. Those come back as identity and topology with no state, and
// are named separately as interruptions.
package execgraph
