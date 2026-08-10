package main

import (
	"strings"
	"testing"
)

func faultedRun(id string, injected, afterFault int, passed bool) result {
	r := result{task: task{ID: id}, Attempt: 1, Passed: passed}
	r.Meter = &meterUsage{Requests: 10, Injected: injected, RequestsAfterFault: afterFault}
	return r
}

func TestFaultCadenceScalesWithTheRun(t *testing.T) {
	script, err := parseFaultScript("every:5:500")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	for _, index := range []int{5, 10, 15} {
		if status, ok := script.statusFor(index); !ok || status != 500 {
			t.Fatalf("request %d = %d/%v, want an injected 500", index, status, ok)
		}
	}
	for _, index := range []int{1, 4, 6, 9} {
		if _, ok := script.statusFor(index); ok {
			t.Fatalf("request %d must be forwarded", index)
		}
	}
}

// An absolute index is a targeted failure; a cadence is background pressure.
// The targeted one must stay exactly where it was asked for.
func TestAbsoluteFaultWinsOverTheCadence(t *testing.T) {
	script, err := parseFaultScript("10:429,every:5:500")
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if status, _ := script.statusFor(10); status != 429 {
		t.Fatalf("request 10 = %d, want the targeted 429", status)
	}
	if status, _ := script.statusFor(5); status != 500 {
		t.Fatalf("request 5 = %d, want the cadence 500", status)
	}
}

func TestFaultScriptRejectsMalformedCadence(t *testing.T) {
	for _, bad := range []string{"every:5", "every:0:500", "every:x:500", "every:5:200"} {
		if _, err := parseFaultScript(bad); err == nil {
			t.Fatalf("%q must be rejected", bad)
		}
	}
}

func TestMeterCountsRequestsAfterAFault(t *testing.T) {
	upstream := okUpstream()
	script, _ := parseFaultScript("2:429")
	m, base, stop := meterAgainst(t, upstream, script)
	defer stop()
	for range 4 {
		post(t, base, "/chat/completions", `{"model":"x"}`).Body.Close()
	}
	got := m.snapshot()
	if got.Injected != 1 {
		t.Fatalf("injected = %d, want 1", got.Injected)
	}
	if got.RequestsAfterFault != 2 {
		t.Fatalf("requests after fault = %d, want 2 (requests 3 and 4)", got.RequestsAfterFault)
	}
}

func TestMeterCountsNoRequestsAfterFaultWhenTheHarnessGivesUp(t *testing.T) {
	script, _ := parseFaultScript("2:429")
	m, base, stop := meterAgainst(t, okUpstream(), script)
	defer stop()
	for range 2 {
		post(t, base, "/chat/completions", `{"model":"x"}`).Body.Close()
	}
	if got := m.snapshot(); got.RequestsAfterFault != 0 {
		t.Fatalf("requests after fault = %d, want 0 — nothing followed the failure", got.RequestsAfterFault)
	}
}

func TestFaultRecoverySeparatesRetryingFromSolving(t *testing.T) {
	got := renderFaultRecovery([]result{
		faultedRun("solved-through", 2, 5, true),
		faultedRun("retried-but-lost", 1, 4, false),
		faultedRun("gave-up", 1, 0, false),
		faultedRun("never-faulted", 0, 0, true),
	})
	for _, want := range []string{
		"**Fault recovery** (3 runs failed on purpose, 4 injections)",
		"**retried** 67% (2)",
		"**still solved** 33% (1/3)",
		"in-run control 100% (1/1 never hit a fault)",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("recovery line missing %q:\n%s", want, got)
		}
	}
}

func TestFaultRecoveryRendersNothingWithoutInjections(t *testing.T) {
	if got := renderFaultRecovery([]result{faultedRun("clean", 0, 0, true)}); got != "" {
		t.Fatalf("want no section when nothing was injected, got:\n%s", got)
	}
}
