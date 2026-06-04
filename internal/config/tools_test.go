package config

import "testing"

func TestBashTimeoutSecondsNormalizesNegative(t *testing.T) {
	cfg := Default()
	cfg.Tools.BashTimeoutSeconds = -1
	if got := cfg.BashTimeoutSeconds(); got != 0 {
		t.Fatalf("BashTimeoutSeconds() = %d, want 0", got)
	}
}
