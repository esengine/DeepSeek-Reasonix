package config

import "testing"

func TestDefaultAutoPlanOff(t *testing.T) {
	if got := Default().Agent.AutoPlan; got != "off" {
		t.Fatalf("default auto_plan = %q, want off", got)
	}
}

func TestDefaultReasoningLanguageAuto(t *testing.T) {
	if got := Default().ReasoningLanguage(); got != "auto" {
		t.Fatalf("default reasoning_language = %q, want auto", got)
	}
}

func TestDefaultMemoryCompilerEnabled(t *testing.T) {
	cfg := Default()
	if !cfg.MemoryCompilerEnabled() {
		t.Fatal("default memory compiler = false, want true")
	}
	if got := cfg.MemoryCompilerVerbosity(); got != MemoryCompilerVerbosityObserve {
		t.Fatalf("default memory compiler verbosity = %q, want observe", got)
	}
}

func TestMaxParallelReadToolsDefaultAndClamp(t *testing.T) {
	cfg := Default()
	if got := cfg.MaxParallelReadTools(); got != DefaultMaxParallelReadTools {
		t.Fatalf("default max_parallel_read_tools = %d, want %d", got, DefaultMaxParallelReadTools)
	}
	cfg.Agent.MaxParallelReadTools = 2
	if got := cfg.MaxParallelReadTools(); got != 2 {
		t.Fatalf("configured max_parallel_read_tools = %d, want 2", got)
	}
	cfg.Agent.MaxParallelReadTools = 99
	if got := cfg.MaxParallelReadTools(); got != MaxParallelReadToolsLimit {
		t.Fatalf("clamped max_parallel_read_tools = %d, want %d", got, MaxParallelReadToolsLimit)
	}
	cfg.Agent.MaxParallelReadTools = -1
	if got := cfg.MaxParallelReadTools(); got != DefaultMaxParallelReadTools {
		t.Fatalf("non-positive max_parallel_read_tools = %d, want default %d", got, DefaultMaxParallelReadTools)
	}
}

func TestDefaultDesktopAppearanceAutoGraphite(t *testing.T) {
	cfg := Default()
	if got := cfg.DesktopTheme(); got != "auto" {
		t.Fatalf("default desktop theme = %q, want auto", got)
	}
	if got := cfg.DesktopThemeStyle(); got != "" {
		t.Fatalf("default desktop theme style = %q, want empty so frontend resolves graphite", got)
	}
}

func TestDefaultDesktopMetricsOn(t *testing.T) {
	cfg := Default()
	if !cfg.DesktopMetrics() {
		t.Fatal("default desktop metrics = false, want true")
	}
	disabled := false
	cfg.Desktop.Metrics = &disabled
	if cfg.DesktopMetrics() {
		t.Fatal("desktop metrics explicit false = true, want false")
	}
}
