package main

import (
	"strings"
	"testing"
)

func meteredRun(id string, meterPrompt, meterCompletion, selfPrompt, selfCompletion int) result {
	r := result{task: task{ID: id}, Attempt: 1}
	r.Meter = &meterUsage{
		Requests: 4, PromptTokens: meterPrompt, CompletionTokens: meterCompletion,
		CacheHitTokens: meterPrompt / 2, CacheMissTokens: meterPrompt - meterPrompt/2,
	}
	r.PromptTokens = selfPrompt
	r.CompletionTokens = selfCompletion
	return r
}

func TestMeterAccountingReportsAgreement(t *testing.T) {
	got := renderMeterAccounting([]result{
		meteredRun("a", 1000, 200, 1000, 200),
		meteredRun("b", 500, 100, 500, 100),
	})
	for _, want := range []string{
		"**Metered at the boundary** (2 runs)",
		"tokens 1,800",
		"cache hit 50%",
		"**self-report divergence** +0.0%",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("meter line missing %q:\n%s", want, got)
		}
	}
}

// The divergence is the publishability gate: if the proxy and the harness
// disagree about the same traffic, one of them is wrong.
func TestMeterAccountingSurfacesDisagreement(t *testing.T) {
	got := renderMeterAccounting([]result{meteredRun("a", 1000, 0, 800, 0)})
	if !strings.Contains(got, "-20.0%") {
		t.Fatalf("want the harness's under-report surfaced:\n%s", got)
	}
}

func TestMeterAccountingSurfacesUnmeasuredAndFaults(t *testing.T) {
	r := meteredRun("a", 100, 10, 100, 10)
	r.Meter.WithoutUsage = 2
	r.Meter.Injected = 1
	got := renderMeterAccounting([]result{r})
	if !strings.Contains(got, "**responses without usage** 2") {
		t.Fatalf("unmeasured responses must be visible:\n%s", got)
	}
	if !strings.Contains(got, "injected faults 1") {
		t.Fatalf("injected faults must be visible:\n%s", got)
	}
}

// An unaccounted run has no harness numbers to compare, so it must not drag the
// divergence toward zero by contributing a silent 0.
func TestMeterAccountingSkipsUnaccountedRunsInTheComparison(t *testing.T) {
	lost := meteredRun("lost", 1000, 0, 0, 0)
	lost.Unaccounted = true
	got := renderMeterAccounting([]result{meteredRun("a", 1000, 0, 1000, 0), lost})
	if !strings.Contains(got, "+0.0% (harness 1,000 vs meter 1,000 over 1 runs)") {
		t.Fatalf("the comparison must drop the unaccounted run from BOTH sides:\n%s", got)
	}
	if !strings.Contains(got, "tokens 2,000") {
		t.Fatalf("real spend still includes the unaccounted run:\n%s", got)
	}
}

func TestMeterAccountingRendersNothingUnmetered(t *testing.T) {
	if got := renderMeterAccounting([]result{{task: task{ID: "a"}, Attempt: 1}}); got != "" {
		t.Fatalf("want no section when nothing was metered, got:\n%s", got)
	}
}

func TestStartTaskMeterIsOffByDefault(t *testing.T) {
	env, m, stop, err := startTaskMeter(suiteConfig{})
	if err != nil || env != nil || m != nil || stop != nil {
		t.Fatalf("metering must be opt-in: env=%v meter=%v err=%v", env, m, err)
	}
}

func TestStartTaskMeterPointsTheChildAtTheProxy(t *testing.T) {
	cfg := suiteConfig{meterConfig: writeConfig(t, twoProviderConfig), model: "kimi-k2"}
	env, m, stop, err := startTaskMeter(cfg)
	if err != nil {
		t.Fatalf("startTaskMeter: %v", err)
	}
	defer stop()
	if m == nil || len(env) != 1 || !strings.HasPrefix(env[0], "REASONIX_HOME=") {
		t.Fatalf("env = %v, want a redirected REASONIX_HOME", env)
	}
	home := strings.TrimPrefix(env[0], "REASONIX_HOME=")
	providers := readProviders(t, home)
	for _, p := range providers {
		if name, _ := p["name"].(string); name == "kimi" {
			base, _ := p["base_url"].(string)
			if !strings.HasPrefix(base, "http://127.0.0.1:") {
				t.Fatalf("kimi base_url = %q, want the loopback meter", base)
			}
			return
		}
	}
	t.Fatalf("kimi provider missing from the metered home: %v", providers)
}
