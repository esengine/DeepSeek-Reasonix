package main

// hostEventNames lists every static event name this package emits through
// the runtime event bridge; the host contract digest covers it. Per-tab
// remote names ("remote-tab:<id>:state", "remote-tab:<id>:event") are
// derived at runtime and are not listed. host_events_test.go pins each
// entry to a string literal in the sources.
var hostEventNames = []string{
	"InboxChanged",
	"agent:event",
	"agent:ready",
	"config:load-warnings",
	"desktop:shell-status",
	"history-index:changed-v1",
	"project-tree:changed",
	"project-tree:changed-v2",
	"project-tree:runtime-changed",
	"remote-tab:opened",
	"remote-tab:updated",
	"remote:forwards",
	"remote:server",
	"remote:status",
	"runtime-state:changed",
	"runtime:rebuilt",
	"session:active-version-changed",
	"session:recovered",
	"session:recovery-failed",
	"tab:meta",
	"terminal:exit",
	"terminal:output",
	"topic:activation",
	"updater:progress",
}
