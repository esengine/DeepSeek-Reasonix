// Package completion assembles the host-authored completion report: the
// evidence-backed answer to "is this done, and what is not proven?".
//
// The model never authors this record. It may claim a task is complete, but
// every factual field here is derived from the task contract and the evidence
// ledger: which acceptance criteria carry fresh proof, which paths the turn
// changed, which verification commands ran and how they exited. What the
// report adds beyond the contract is subtraction — a change nobody looked at
// again, a mutation no verification followed, a command that passed only
// before the latest edit. Silence cannot hide those, because the host computes
// them instead of reading them off a claim.
//
// A report is therefore not a gate. Its verdict separates work that lacks
// proof (Incomplete) from work that is proven but carries declared gaps
// (Partial); the latter is a legitimate terminal state. The contract decides
// whether the agent may stop, the report decides what the user is told when it
// does.
//
// Building a report makes no model call and mutates nothing.
package completion
