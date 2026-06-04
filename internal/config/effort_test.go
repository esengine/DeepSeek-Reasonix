package config

import (
	"testing"
)

func TestResolveEffort_ExactMatch(t *testing.T) {
	caps := EffortCapability{Supported: true, Levels: []string{"auto", "low", "medium", "high"}, Default: "auto"}
	for _, level := range []string{"low", "medium", "high"} {
		res := ResolveEffort(caps, level)
		if res.Effort != level || res.Degraded || res.Blocked {
			t.Errorf("ResolveEffort(%q) = %+v, want Effort=%q, Degraded=false, Blocked=false", level, res, level)
		}
	}
}

func TestResolveEffort_AutoAlwaysEmpty(t *testing.T) {
	caps := EffortCapability{Supported: true, Levels: []string{"auto", "low", "high"}, Default: "auto"}
	for _, input := range []string{"auto", "AUTO", "Auto", ""} {
		res := ResolveEffort(caps, input)
		if res.Effort != "" || res.Degraded || res.Blocked {
			t.Errorf("ResolveEffort(%q) = %+v, want Effort=\"\", Degraded=false, Blocked=false", input, res)
		}
	}
}

func TestResolveEffort_DegradeToDefault(t *testing.T) {
	caps := EffortCapability{Supported: true, Levels: []string{"auto", "low", "medium", "high"}, Default: "high"}
	res := ResolveEffort(caps, "max")
	if res.Effort != "high" || !res.Degraded || res.Warning == "" {
		t.Errorf("ResolveEffort(max) = %+v, want Effort=high, Degraded=true, Warning non-empty", res)
	}
}

func TestResolveEffort_DegradeToAuto(t *testing.T) {
	caps := EffortCapability{Supported: true, Levels: []string{"auto", "low", "high"}, Default: "auto"}
	res := ResolveEffort(caps, "max")
	if res.Effort != "" || !res.Degraded {
		t.Errorf("ResolveEffort(max) = %+v, want Effort=\"\", Degraded=true", res)
	}
}

func TestResolveEffort_DegradeToFirstLevel(t *testing.T) {
	caps := EffortCapability{Supported: true, Levels: []string{"low", "medium", "high"}, Default: "turbo"}
	res := ResolveEffort(caps, "max")
	if res.Effort != "low" || !res.Degraded {
		t.Errorf("ResolveEffort(max) = %+v, want Effort=low, Degraded=true", res)
	}
}

func TestResolveEffort_Blocked(t *testing.T) {
	caps := EffortCapability{}
	res := ResolveEffort(caps, "high")
	if !res.Blocked || res.Effort != "" {
		t.Errorf("ResolveEffort(high) on blocked caps = %+v, want Blocked=true, Effort=\"\"", res)
	}
}

func TestResolveEffort_CaseInsensitive(t *testing.T) {
	caps := EffortCapability{Supported: true, Levels: []string{"auto", "low", "high"}, Default: "auto"}
	res := ResolveEffort(caps, "HIGH")
	if res.Effort != "high" || res.Degraded {
		t.Errorf("ResolveEffort(HIGH) = %+v, want Effort=high, Degraded=false", res)
	}
}

func TestResolveEffort_SupportedButEmptyLevels(t *testing.T) {
	caps := EffortCapability{Supported: true, Levels: []string{}, Default: "auto"}
	res := ResolveEffort(caps, "high")
	if !res.Degraded {
		t.Errorf("ResolveEffort(high) on empty levels = %+v, want Degraded=true", res)
	}
}

func TestSession_AdaptToProvider(t *testing.T) {
	caps := EffortCapability{Supported: true, Levels: []string{"auto", "low", "medium", "high"}, Default: "auto"}

	// Valid effort passes through.
	s := Session{Effort: "high"}
	warning := s.AdaptToProvider(caps)
	if s.Effort != "high" || warning != "" {
		t.Errorf("AdaptToProvider with high: effort=%q, warning=%q", s.Effort, warning)
	}

	// Auto clears to "".
	s = Session{Effort: "auto"}
	warning = s.AdaptToProvider(caps)
	if s.Effort != "" || warning != "" {
		t.Errorf("AdaptToProvider with auto: effort=%q, warning=%q", s.Effort, warning)
	}

	// Unsupported degrades.
	s = Session{Effort: "max"}
	warning = s.AdaptToProvider(caps)
	if s.Effort == "max" || warning == "" {
		t.Errorf("AdaptToProvider with max: effort=%q, warning=%q", s.Effort, warning)
	}

	// Blocked clears.
	capsBlocked := EffortCapability{}
	s = Session{Effort: "high"}
	warning = s.AdaptToProvider(capsBlocked)
	if s.Effort != "" || warning == "" {
		t.Errorf("AdaptToProvider blocked: effort=%q, warning=%q", s.Effort, warning)
	}
}
