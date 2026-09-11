package main

import (
	"fmt"
	"os"
	"strings"
)

// meterSettings validates the metering flags together: a fault script has
// nowhere to be injected without the proxy, so asking for one without -meter
// is a mistake worth stopping for rather than silently ignoring.
func meterSettings(configPath, faultSpec string) (string, faultScript) {
	faults, err := parseFaultScript(faultSpec)
	if err != nil {
		fmt.Fprintln(os.Stderr, "faults:", err)
		os.Exit(2)
	}
	if !faults.empty() && strings.TrimSpace(configPath) == "" {
		fmt.Fprintln(os.Stderr, "faults: -faults needs -meter; the proxy is where a fault can be injected")
		os.Exit(2)
	}
	return configPath, faults
}

// runMeter is one run's metering: the environment that points the child at the
// proxy, and the proxy itself. The zero value is a working no-op, so an
// unmetered run needs no branch at the call site.
type runMeter struct {
	env  []string
	m    *meter
	stop func()
}

// attachMeter starts the run's meter, folding a setup failure into the run's
// note instead of aborting the suite: an unmetered run is still a data point.
func attachMeter(cfg suiteConfig, r *result) runMeter {
	env, m, stop, err := startTaskMeter(cfg)
	if err != nil {
		r.Note = strings.TrimSpace(r.Note + " meter: " + err.Error())
	}
	return runMeter{env: env, m: m, stop: stop}
}

func (rm runMeter) close() {
	if rm.stop != nil {
		rm.stop()
	}
}

// record attaches what the proxy observed. It stays separate from close so the
// snapshot is taken while the run's numbers are still being assembled.
func (rm runMeter) record(r *result) {
	if rm.m == nil {
		return
	}
	observed := rm.m.snapshot()
	r.Meter = &observed
}

// startTaskMeter brings up a per-task meter and returns the environment that
// points the child at it. One meter per task keeps spend attributable to the
// task that incurred it. A nil meter means metering is off, which is the
// default: the proxy carries provider credentials and must be opt-in.
func startTaskMeter(cfg suiteConfig) (env []string, m *meter, stop func(), err error) {
	if strings.TrimSpace(cfg.meterConfig) == "" {
		return nil, nil, nil, nil
	}
	upstream, err := meterUpstream(cfg.meterConfig, cfg.model)
	if err != nil {
		return nil, nil, nil, err
	}
	m, err = newMeter(upstream, cfg.meterFaults)
	if err != nil {
		return nil, nil, nil, err
	}
	base, stopServer, err := m.serve()
	if err != nil {
		return nil, nil, nil, err
	}
	home, err := os.MkdirTemp("", "e2ebench-meter-home-")
	if err != nil {
		stopServer()
		return nil, nil, nil, err
	}
	if err := writeMeteredConfig(cfg.meterConfig, home, cfg.model, base); err != nil {
		stopServer()
		_ = os.RemoveAll(home)
		return nil, nil, nil, err
	}
	return []string{"REASONIX_HOME=" + home}, m, func() {
		stopServer()
		_ = os.RemoveAll(home)
	}, nil
}

// renderMeterAccounting reports what the neutral proxy measured and how far the
// harness's own accounting drifted from it. Divergence is the number that
// decides whether a cross-harness comparison is publishable at all: if the
// proxy and the harness disagree about the same run, one of them is wrong.
func renderMeterAccounting(results []result) string {
	metered, selfReported, unmeasured, injected := 0, 0, 0, 0
	var meterTokens, harnessTokens, comparableTokens int
	var hit, miss int
	for _, r := range results {
		if r.Skipped || r.Meter == nil {
			continue
		}
		metered++
		meterTokens += r.Meter.PromptTokens + r.Meter.CompletionTokens
		hit += r.Meter.CacheHitTokens
		miss += r.Meter.CacheMissTokens
		unmeasured += r.Meter.WithoutUsage
		injected += r.Meter.Injected
		// A run whose metrics file never landed has no harness number, so it
		// stays out of both sides of the comparison; folding its meter tokens
		// against a silent zero would invent a divergence.
		if !r.Unaccounted {
			selfReported++
			harnessTokens += r.PromptTokens + r.CompletionTokens
			comparableTokens += r.Meter.PromptTokens + r.Meter.CompletionTokens
		}
	}
	if metered == 0 {
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "**Metered at the boundary** (%d runs): tokens %s · cache hit %s",
		metered, comma(meterTokens), pct(hit, hit+miss))
	if injected > 0 {
		fmt.Fprintf(&b, " · injected faults %d", injected)
	}
	if unmeasured > 0 {
		fmt.Fprintf(&b, " · **responses without usage** %d", unmeasured)
	}
	if selfReported > 0 && comparableTokens > 0 {
		fmt.Fprintf(&b, " · **self-report divergence** %s (harness %s vs meter %s over %d runs)",
			divergence(harnessTokens, comparableTokens), comma(harnessTokens), comma(comparableTokens), selfReported)
	}
	return b.String() + "\n\n"
}

// divergence is the harness's own token count as a signed deviation from the
// meter's. Anything but ~0 means the two disagree about the same traffic.
func divergence(harness, meter int) string {
	if meter == 0 {
		return "—"
	}
	delta := float64(harness-meter) / float64(meter) * 100
	return fmt.Sprintf("%+.1f%%", delta)
}
