// Package execjournal is the durable record that a delegated execution existed.
//
// A transcript is appended when a turn ends, so a process that dies inside one
// takes the whole turn with it: the request, the dispatch, and every item it
// opened. A fan-out's children are therefore invisible to the next process
// unless they finished — the ones that were executing have never been written
// down anywhere, which a probe measured as neither interrupted nor lost but
// simply absent.
//
// This journal is written before the work becomes observable, the same order an
// adjudication barrier is recorded in. Nothing here decides what an execution
// is, who may run one, or what it produced: an entry says a delegation was
// opened, under whose turn, with which grant, and whether the orchestration has
// since let go of it. Its result stays where it already lives, in the sub-agent
// store, so no two files can disagree about how a child ended.
//
// An entry proves that work entered orchestration. It does not prove that the
// work started, that it ran, or that anything about it can be resumed.
//
// A queued entry proves the scheduler initially denied admission after the item
// became ready. Its cause records that first denial; it does not describe the
// blocker that remained immediately before execution, which can be a different
// one by then.
//
// Interrupted is never written. It is derived: an open entry with no live owner
// in this process. Recording it would mean the dying process wrote down what it
// could not know.
package execjournal
