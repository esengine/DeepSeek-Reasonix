package main

import (
	"time"

	"reasonix/internal/control"
	"reasonix/internal/jobs"
)

// Interactive removal returns promptly; durable cleanup-pending markers let
// delayed cleanup safely own non-cooperative jobs after this grace expires.
const desktopSessionRemovalGrace = time.Second
const desktopSessionRemovalWatchdog = desktopSessionRemovalGrace + 250*time.Millisecond

func waitDestroyHandles(destroys []control.SessionDestroyHandle) bool {
	return waitDestroyHandleBatches([][]control.SessionDestroyHandle{destroys})[0]
}

func waitDestroyHandleBatches(batches [][]control.SessionDestroyHandle) []bool {
	timedOut := make([]bool, len(batches))
	if len(batches) == 0 {
		return timedOut
	}
	type batchResult struct {
		index    int
		timedOut bool
	}
	deadline := time.Now().Add(desktopSessionRemovalWatchdog)
	results := make(chan batchResult, len(batches))
	for i, destroys := range batches {
		go func() {
			results <- batchResult{index: i, timedOut: waitDestroyHandlesUntil(destroys, deadline)}
		}()
	}
	for range batches {
		result := <-results
		timedOut[result.index] = result.timedOut
	}
	return timedOut
}

func waitDestroyHandlesUntil(destroys []control.SessionDestroyHandle, deadline time.Time) bool {
	results := make(chan jobs.TeardownResult, len(destroys))
	waits := 0
	for _, destroy := range destroys {
		wait := destroy.Wait
		if destroy.WaitFor != nil {
			remaining := min(max(time.Until(deadline), time.Duration(0)), desktopSessionRemovalGrace)
			wait = func() jobs.TeardownResult { return destroy.WaitFor(remaining) }
		}
		if wait == nil {
			continue
		}
		waits++
		go func(wait func() jobs.TeardownResult) { results <- wait() }(wait)
	}
	if waits == 0 {
		return false
	}
	remaining := time.Until(deadline)
	if remaining <= 0 {
		return true
	}
	timer := time.NewTimer(remaining)
	defer timer.Stop()
	timedOut := false
	for range waits {
		select {
		case result := <-results:
			timedOut = timedOut || result.HasTimedOut()
		case <-timer.C:
			return true
		}
	}
	return timedOut
}
