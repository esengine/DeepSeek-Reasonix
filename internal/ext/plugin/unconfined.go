package plugin

import "sync/atomic"

var unconfinedLaunch atomic.Bool

// LaunchedUnconfinedProcess reports whether this process has ever started a
// local MCP server outside the OS command sandbox. It never resets: a server
// the model could drive while it ran may already have written anywhere its
// user can, and disconnecting it undoes none of that.
func LaunchedUnconfinedProcess() bool { return unconfinedLaunch.Load() }
