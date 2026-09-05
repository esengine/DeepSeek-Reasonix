package main

import (
	"strings"
	"testing"
)

// Sentinels are matched by substring, so one contained in another dispatches on
// the same prompt: the identity arm silently ran the cancellation fan-out for as
// long as PROBE-PARALLEL sat inside PROBE-PARALLEL-IDENTITY, and its rows read a
// scenario nobody wrote. Care did not catch it — the child sentinels carry a
// comment about exactly this hazard and the parent ones had no guard at all.
func TestNoSentinelContainsAnother(t *testing.T) {
	sentinels := map[string]string{
		"fleetWarm": fleetWarmSentinel, "fleetMixed": fleetMixedSentinel,
		"fleetPair": fleetPairSentinel, "fleetHolder": fleetHolderSentinel,
		"fleetRefused": fleetRefusedSentinel, "fleetTerminal": fleetTerminalSentinel,
		"fleetOutcomes": fleetOutcomesSentinel, "fleetDerive": fleetDeriveSentinel,
		"fleetIdentity": fleetIdentitySentinel, "fleetActive": fleetActiveSentinel,
		"fleetSettled": fleetSettledSentinel, "fleetLive": fleetLiveSentinel,
		"parallel": parallelSentinel, "parallelIdentity": parallelIdentity,
		"task": taskSentinel, "sleep": sleepSentinel,
		"ask": askSentinel, "retodo": retodoSentinel,
		"childDone": childDone, "childHang": childHang, "childFail": childFail,
		"childHold": childHold, "childRelease": childRelease, "childFailLate": childFailLate,
	}
	for outerName, outer := range sentinels {
		for innerName, inner := range sentinels {
			if outerName == innerName {
				continue
			}
			if strings.Contains(outer, inner) {
				t.Errorf("%s (%q) contains %s (%q): a prompt naming the first dispatches the second too",
					outerName, outer, innerName, inner)
			}
		}
	}
}
