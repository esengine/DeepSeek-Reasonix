package cli

import (
	"testing"

	"reasonix/internal/contract/config"
)

func TestCompactRatioAcceptsLegacyLowerRange(t *testing.T) {
	for _, value := range []string{"30", "64"} {
		t.Run(value, func(t *testing.T) {
			isolateCLIConfigHome(t)
			captureStdout(t, func() {
				if rc := Run([]string{"config", "compact-ratio", value}, "test"); rc != 0 {
					t.Fatalf("compact-ratio %s returned %d", value, rc)
				}
			})
			cfg := config.LoadForEdit(config.UserConfigPath())
			if cfg.Agent.CompactRatio < 0.30 || cfg.Agent.CompactRatio >= 0.65 {
				t.Fatalf("saved ratio = %v", cfg.Agent.CompactRatio)
			}
		})
	}
}
