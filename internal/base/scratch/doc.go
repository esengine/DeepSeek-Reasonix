// Package scratch owns temp directories that belong to one running process.
//
// A directory is claimed by an exclusive lock on a file inside it, held for the
// owner's lifetime. The operating system drops that lock when the owner dies
// for any reason, so "no live owner" is answered by trying the lock, never by
// a name or an age. Sweep removes only directories whose lock it can take, and
// a Remove that Windows refuses leaves a directory the next Sweep can prove
// unowned.
//
// The lock is internal/base/filelock, the same mechanism sessiontemp uses for
// its per-session directories, so there is one notion of "owned" for temp
// directories.
//
// Create claims a new directory; Remove releases the claim before deleting, so
// the delete never races a lock handle, and retries for up to two seconds
// while a file inside is still open.
//
// Sweep and SweepOnce touch only direct children of the given root that carry
// the given prefix, are real directories (a symlink is never followed), are
// older than a one-minute creation grace, and whose lock is free. The grace
// covers the gap between mkdir and the owner taking its lock; it is not a
// staleness rule. A directory with no lock file at all is unowned.
package scratch
