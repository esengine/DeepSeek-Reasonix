// Package schedrun runs one claimed scheduled run in a child process and holds
// it to its budget.
//
// The child is the binary itself under a fixed read-only posture. Its posture
// and its secret-protection switches are process-wide state, so a run gets a
// process of its own rather than a controller beside an interactive one. The
// Supervisor starts it, counts the tokens it reports, and ends it, tree and
// all, when the wall clock or the token ceiling passes; a child that ignores
// every request to stop is killed, not asked again.
//
// Parent and child talk over the child's standard streams: one JSON line per
// usage report and a last line carrying the result upward, one "go" line
// downward. The child keeps reading downward for as long as it runs, and the end
// of that stream is the supervisor going away, so no supervisor crash leaves a
// run spending. Anything the supervisor cannot account for is charged at the
// run's whole cap.
package schedrun
