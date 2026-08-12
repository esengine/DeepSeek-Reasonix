//go:build linux && cgo && reasonix_webkit_smoke

package main

import (
	"os"
	"os/exec"
	"strconv"
	"testing"
)

const webKitSmokeModeEnv = "REASONIX_WEBKIT_SMOKE_MODE"

func TestWebKitNativeRecoverySmoke(t *testing.T) {
	tests := []struct {
		name       string
		mode       int
		recoveries []int
		reloads    int
	}{
		{name: "success", mode: webKitSmokeSuccess, recoveries: []int{webKitRecoverySucceeded}, reloads: 1},
		{name: "failure", mode: webKitSmokeFailure, recoveries: []int{webKitRecoveryFailed}, reloads: 1},
		{name: "timeout", mode: webKitSmokeTimeout, recoveries: []int{webKitRecoveryFailed}, reloads: 1},
		{name: "cooldown", mode: webKitSmokeCooldown, recoveries: []int{webKitRecoverySucceeded, webKitRecoveryNotApplicable}, reloads: 1},
	}
	if rawMode := os.Getenv(webKitSmokeModeEnv); rawMode != "" {
		mode, err := strconv.Atoi(rawMode)
		if err != nil {
			t.Fatalf("invalid native smoke mode %q: %v", rawMode, err)
		}
		for _, test := range tests {
			if test.mode == mode {
				assertWebKitNativeRecovery(t, test.mode, test.recoveries, test.reloads)
				return
			}
		}
		t.Fatalf("unknown native smoke mode %d", mode)
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			command := exec.Command(os.Args[0], "-test.run", "^TestWebKitNativeRecoverySmoke$")
			command.Env = append(os.Environ(), webKitSmokeModeEnv+"="+strconv.Itoa(test.mode))
			if output, err := command.CombinedOutput(); err != nil {
				t.Fatalf("native smoke child failed: %v\n%s", err, output)
			}
		})
	}
}

func assertWebKitNativeRecovery(t *testing.T, mode int, recoveries []int, wantReloads int) {
	t.Helper()
	result, events, reloads := runWebKitNativeSmoke(mode)
	if result != 0 {
		t.Fatalf("native smoke result = %d", result)
	}
	if reloads != wantReloads {
		t.Fatalf("reload count = %d, want %d", reloads, wantReloads)
	}
	if len(events) != len(recoveries) {
		t.Fatalf("events = %+v, want %d", events, len(recoveries))
	}
	for i, event := range events {
		if event.reason != 2 {
			t.Errorf("event %d reason = %d, want terminated_by_api", i, event.reason)
		}
		if event.recovery != recoveries[i] {
			t.Errorf("event %d recovery = %d, want %d", i, event.recovery, recoveries[i])
		}
	}
}
