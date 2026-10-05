// Package sessionstore owns a conversation's durable record: the in-memory
// Session, its .jsonl checkpoint and event log, the sidecars that answer a
// listing without replaying it, branches, leases and recovery copies, and the
// compaction and subagent records saved beside a transcript.
//
// It knows nothing about the loop that fills a session. The agent drives
// turns and decides what to fold; this package decides only how a transcript
// reaches disk, how a torn or foreign write is recognised, and how it is read
// back.
package sessionstore
