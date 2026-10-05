// Package observation takes WorkspaceSnapshots under a host-owned Policy
// (docs/design/TRUSTED_EXECUTION.md §3.7). A snapshot is a tree of directory
// nodes stored as content-addressed objects, so an unchanged directory costs
// nothing to record again and two snapshots diff by walking only the subtrees
// whose digests differ.
//
// A file is identified by its stamp — size, modification time, and where the
// platform has them, inode change time, inode and device — not by hashing its
// bytes. Change time cannot be set from user space, so any write shows; the
// price is that a write restoring identical bytes still counts as a change.
// Content digests belong to the paths a verdict names, not to every file.
//
// Only the Policy may exclude a path. Ignore files and workspace config are
// never read: whatever the model can write cannot narrow what is observed.
package observation
