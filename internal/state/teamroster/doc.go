// Package teamroster is the durable record of a user's team: the long-lived
// agents, the group chats they meet in, the messages they send each other and
// where each group's turn-taking stands. It decides nothing about delivery or
// turns; internal/session/team does, and reaches the disk only through here.
//
// The roster is one file, rewritten whole and atomically under a cross-process
// lock, and every write bumps a revision a caller may hold its update to.
// Validation runs before every write, so a roster on disk is one this build
// accepts: a file that fails it is reported as corrupt and never repaired,
// because it is the user's data. A roster written by a newer build is
// readable and refuses writes.
//
// Exchanges and each group's log are append-only lines. A line a crash cut
// short has no newline; readers skip it and the next append cuts it away, so
// a torn write costs that one entry and never the file. The exchange record is
// folded by id into each exchange's latest state, and a request id the sender
// reuses is a retry only when the message is the same. A group's round state
// is rewritten whole, under the lock its log is appended under.
//
// One process at a time is the team's writer (AcquireLease); any other shows
// the team read-only. Agents, groups and exchanges are addressed by random ids
// that are checked before they reach a path, never by name, so a rename moves
// nothing on disk.
package teamroster
