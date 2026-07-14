package config

import (
	"testing"

	"github.com/BurntSushi/toml"
)

func TestBashTimeoutSecondsDefaultsToSafetyCap(t *testing.T) {
	cfg := Default()
	if cfg.Tools.BashTimeoutSeconds != nil {
		t.Fatalf("default raw bash timeout = %v, want nil", *cfg.Tools.BashTimeoutSeconds)
	}
	if got := cfg.BashTimeoutSeconds(); got != 120 {
		t.Fatalf("BashTimeoutSeconds() = %d, want 120", got)
	}
}

func TestBashTimeoutSecondsAllowsExplicitZero(t *testing.T) {
	cfg := Default()
	cfg.Tools.BashTimeoutSeconds = intPtr(0)
	if got := cfg.BashTimeoutSeconds(); got != 0 {
		t.Fatalf("BashTimeoutSeconds() = %d, want 0", got)
	}
}

func TestBashTimeoutSecondsParsesExplicitZero(t *testing.T) {
	cfg := Default()
	if _, err := toml.Decode("[tools]\nbash_timeout_seconds = 0\n", cfg); err != nil {
		t.Fatalf("decode explicit zero: %v", err)
	}
	if cfg.Tools.BashTimeoutSeconds == nil {
		t.Fatal("explicit zero decoded as nil")
	}
	if got := cfg.BashTimeoutSeconds(); got != 0 {
		t.Fatalf("BashTimeoutSeconds() = %d, want 0", got)
	}
}

func TestBashTimeoutSecondsFallsBackForNegative(t *testing.T) {
	cfg := Default()
	cfg.Tools.BashTimeoutSeconds = intPtr(-1)
	if got := cfg.BashTimeoutSeconds(); got != 120 {
		t.Fatalf("BashTimeoutSeconds() = %d, want 120", got)
	}
}

func TestMCPCallTimeoutSecondsDefaultsToSafetyCap(t *testing.T) {
	cfg := Default()
	if cfg.Tools.MCPCallTimeoutSeconds != nil {
		t.Fatalf("default raw MCP call timeout = %v, want nil", *cfg.Tools.MCPCallTimeoutSeconds)
	}
	if got := cfg.MCPCallTimeoutSeconds(); got != 300 {
		t.Fatalf("MCPCallTimeoutSeconds() = %d, want 300", got)
	}
}

func TestMCPCallTimeoutSecondsExplicitPositive(t *testing.T) {
	cfg := Default()
	if _, err := toml.Decode("[tools]\nmcp_call_timeout_seconds = 600\n", cfg); err != nil {
		t.Fatalf("decode MCP timeout: %v", err)
	}
	if cfg.Tools.MCPCallTimeoutSeconds == nil {
		t.Fatal("explicit MCP timeout decoded as nil")
	}
	if got := cfg.MCPCallTimeoutSeconds(); got != 600 {
		t.Fatalf("MCPCallTimeoutSeconds() = %d, want 600", got)
	}
}

func TestMCPCallTimeoutSecondsFallsBackForZeroOrNegative(t *testing.T) {
	cfg := Default()
	cfg.Tools.MCPCallTimeoutSeconds = intPtr(0)
	if got := cfg.MCPCallTimeoutSeconds(); got != 300 {
		t.Fatalf("zero MCPCallTimeoutSeconds() = %d, want 300", got)
	}
	cfg.Tools.MCPCallTimeoutSeconds = intPtr(-1)
	if got := cfg.MCPCallTimeoutSeconds(); got != 300 {
		t.Fatalf("negative MCPCallTimeoutSeconds() = %d, want 300", got)
	}
}

func TestBackgroundJobStalledWarningSecondsDefault(t *testing.T) {
	cfg := Default()
	if cfg.Tools.BackgroundJobs.StalledWarningSeconds != nil {
		t.Fatalf("default raw stalled warning = %v, want nil", *cfg.Tools.BackgroundJobs.StalledWarningSeconds)
	}
	if got := cfg.BackgroundJobStalledWarningSeconds(); got != 900 {
		t.Fatalf("BackgroundJobStalledWarningSeconds() = %d, want 900", got)
	}
}

func TestBackgroundJobStalledWarningSecondsAllowsExplicitZero(t *testing.T) {
	cfg := Default()
	cfg.Tools.BackgroundJobs.StalledWarningSeconds = intPtr(0)
	if got := cfg.BackgroundJobStalledWarningSeconds(); got != 0 {
		t.Fatalf("BackgroundJobStalledWarningSeconds() = %d, want 0", got)
	}
}

func TestBackgroundJobStalledWarningSecondsParsesExplicitZero(t *testing.T) {
	cfg := Default()
	if _, err := toml.Decode("[tools.background_jobs]\nstalled_warning_seconds = 0\n", cfg); err != nil {
		t.Fatalf("decode explicit zero: %v", err)
	}
	if cfg.Tools.BackgroundJobs.StalledWarningSeconds == nil {
		t.Fatal("explicit zero decoded as nil")
	}
	if got := cfg.BackgroundJobStalledWarningSeconds(); got != 0 {
		t.Fatalf("BackgroundJobStalledWarningSeconds() = %d, want 0", got)
	}
}

func TestBackgroundJobStalledWarningSecondsBounds(t *testing.T) {
	cfg := Default()
	cfg.Tools.BackgroundJobs.StalledWarningSeconds = intPtr(-1)
	if got := cfg.BackgroundJobStalledWarningSeconds(); got != 900 {
		t.Fatalf("negative BackgroundJobStalledWarningSeconds() = %d, want 900", got)
	}
	cfg.Tools.BackgroundJobs.StalledWarningSeconds = intPtr(90000)
	if got := cfg.BackgroundJobStalledWarningSeconds(); got != 86400 {
		t.Fatalf("oversized BackgroundJobStalledWarningSeconds() = %d, want 86400", got)
	}
}

func TestCommandTimeoutSecondsDefaultsToSafetyCap(t *testing.T) {
	cfg := Default()
	if cfg.Tools.CommandTimeoutSeconds != 0 {
		t.Fatalf("default raw command timeout = %d, want 0 (unset)", cfg.Tools.CommandTimeoutSeconds)
	}
	if got := cfg.CommandTimeoutSeconds(); got != 120 {
		t.Fatalf("CommandTimeoutSeconds() = %d, want 120", got)
	}
}

func TestCommandTimeoutSecondsCustomValue(t *testing.T) {
	cfg := Default()
	cfg.Tools.CommandTimeoutSeconds = 600
	if got := cfg.CommandTimeoutSeconds(); got != 600 {
		t.Fatalf("CommandTimeoutSeconds() = %d, want 600", got)
	}
}

func TestCommandTimeoutSecondsFallsBackForZeroOrNegative(t *testing.T) {
	cfg := Default()
	cfg.Tools.CommandTimeoutSeconds = 0
	if got := cfg.CommandTimeoutSeconds(); got != 120 {
		t.Fatalf("zero CommandTimeoutSeconds() = %d, want 120", got)
	}
	cfg.Tools.CommandTimeoutSeconds = -5
	if got := cfg.CommandTimeoutSeconds(); got != 120 {
		t.Fatalf("negative CommandTimeoutSeconds() = %d, want 120", got)
	}
}

func TestCommandTimeoutSecondsParsesFromTOML(t *testing.T) {
	cfg := Default()
	if _, err := toml.Decode("[tools]\ncommand_timeout_seconds = 300\n", cfg); err != nil {
		t.Fatalf("decode command timeout: %v", err)
	}
	if cfg.Tools.CommandTimeoutSeconds != 300 {
		t.Fatalf("decoded command_timeout_seconds = %d, want 300", cfg.Tools.CommandTimeoutSeconds)
	}
	if got := cfg.CommandTimeoutSeconds(); got != 300 {
		t.Fatalf("CommandTimeoutSeconds() = %d, want 300", got)
	}
}

func TestBashTimeoutSecondsPrefersLegacyWhenExplicitlySet(t *testing.T) {
	cfg := Default()
	cfg.Tools.CommandTimeoutSeconds = 600
	cfg.Tools.BashTimeoutSeconds = intPtr(30)
	if got := cfg.BashTimeoutSeconds(); got != 30 {
		t.Fatalf("legacy bash_timeout should win: BashTimeoutSeconds() = %d, want 30", got)
	}
}

func TestBashTimeoutSecondsUsesCommandTimeoutWhenLegacyAbsent(t *testing.T) {
	cfg := Default()
	cfg.Tools.CommandTimeoutSeconds = 600
	if cfg.Tools.BashTimeoutSeconds != nil {
		t.Fatal("legacy bash_timeout should be nil")
	}
	if got := cfg.BashTimeoutSeconds(); got != 600 {
		t.Fatalf("BashTimeoutSeconds() = %d, want 600 (from command_timeout_seconds)", got)
	}
}
