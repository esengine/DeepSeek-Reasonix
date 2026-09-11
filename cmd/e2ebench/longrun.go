package main

import (
	"fmt"
	"strings"
)

// faultRecovery splits a run by whether the meter actually failed it. With a
// cadence, short tasks never reach a fault and form an in-run control group,
// so the cost of failure is measured against the same suite and model rather
// than against a separate arm run at a different time.
type faultRecovery struct {
	faulted, faultedSolved   int
	unfaulted, unfaultSolved int
	keptGoing                int // faulted runs that issued another request
	injected                 int
}

func gatherFaultRecovery(results []result) faultRecovery {
	var f faultRecovery
	for _, r := range results {
		if r.Skipped || r.Meter == nil || r.Attempt > 1 {
			continue
		}
		if r.Meter.Injected == 0 {
			f.unfaulted++
			if r.Passed {
				f.unfaultSolved++
			}
			continue
		}
		f.faulted++
		f.injected += r.Meter.Injected
		if r.Meter.RequestsAfterFault > 0 {
			f.keptGoing++
		}
		if r.Passed {
			f.faultedSolved++
		}
	}
	return f
}

// renderFaultRecovery prices injected failure. Two different things are worth
// separating: whether the harness kept talking after a failure at all, and
// whether the task still landed. A harness can retry forever and still never
// finish, and that is not recovery.
func renderFaultRecovery(results []result) string {
	f := gatherFaultRecovery(results)
	if f.faulted == 0 {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "**Fault recovery** (%d runs failed on purpose, %d injections): **retried** %s (%d) · **still solved** %s (%d/%d)",
		f.faulted, f.injected, pct(f.keptGoing, f.faulted), f.keptGoing,
		pct(f.faultedSolved, f.faulted), f.faultedSolved, f.faulted)
	if f.unfaulted > 0 {
		fmt.Fprintf(&b, " · in-run control %s (%d/%d never hit a fault)",
			pct(f.unfaultSolved, f.unfaulted), f.unfaultSolved, f.unfaulted)
	}
	return b.String() + "\n\n"
}
