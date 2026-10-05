// Package writeclaim decides who may write where while sub-agents run
// concurrently: a WritePathSet is a normalized claim over workspace paths, a
// WriteGrant is the fence one run writes inside, and the SubagentScheduler
// admits runs within concurrency limits and never lets two writers hold
// overlapping claims.
package writeclaim
