// Package schedule is the durable store and budget policy for scheduled runs.
//
// One machine-wide manifest holds schedules and run records, so a claim, its
// dedup key and its budget charge commit in a single atomic write. The package
// starts no process: it decides whether a slot may start (Claim), records what
// it cost (Finish, Reap) and refuses what the policy forbids. Policy values
// come from user-level configuration only; a project layer can tighten them and
// never loosen them (Resolve).
//
// The one number the user layer cannot go under is the 10 minute
// AbsoluteMinInterval, a guard against a typo burning quota. It is not the
// default (60 minutes), so it locks nothing in.
//
// Only the latest slot of a schedule can be claimed: a slot missed while
// nothing ticked is never made up. A run claimed but not yet holding its
// session lease is presumed dead after ReapGrace; if a start is slower than
// that it is reaped wrongly, and MarkRunning then returns ErrRunSettled, which
// the supervisor must treat as "do not run".
//
// The confirmed digest is unkeyed, and no key would change that: a writer that is
// not confined by the operating-system sandbox can read any key a person's
// account can. The store directory is denied to file tools, masked or
// write-denied for sandboxed shell commands, and read-protected for every run,
// which defends the confirmation against sandboxed writers only. On Windows
// (no sandbox), with the sandbox off or under yolo, and for MCP and plugin
// processes, anything that can write the directory can forge a confirmed
// schedule. A frontend must not present "you confirmed this" as tamper-proof
// there.
package schedule
