package sidecar

import "sync/atomic"

var unconfinedLaunch atomic.Bool

// LaunchedUnconfinedProcess reports whether this process has ever tried to
// start an extension sidecar. Sidecars run outside the OS command sandbox, so
// from the first attempt on, a tool the model can call may write anywhere its
// user can. It never resets.
func LaunchedUnconfinedProcess() bool { return unconfinedLaunch.Load() }
