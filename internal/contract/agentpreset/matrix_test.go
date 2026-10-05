package agentpreset_test

import (
	"testing"

	"reasonix/internal/contract/agentpreset"
	"reasonix/internal/runtime/taskpolicy"
)

// TestRoleSettingMatrix locks the product contract for the two role settings:
// the shared tool surface is owned by boot, verification breadth by this
// package, and how much review a change owes by the receipts it produced.
func TestRoleSettingMatrix(t *testing.T) {
	cases := map[agentpreset.AgentPreset]agentpreset.VerificationLevel{
		agentpreset.Balanced: agentpreset.VerifyTargeted,
		agentpreset.Delivery: agentpreset.VerifyFull,
	}
	for preset, verify := range cases {
		t.Run(string(preset), func(t *testing.T) {
			if got := agentpreset.PolicyOf(preset).VerificationPolicy.Level; got != verify {
				t.Fatalf("verify = %v, want %v", got, verify)
			}
			tp := taskpolicy.Derive(taskpolicy.Input{Preset: preset})
			if tp.Preset != preset {
				t.Fatalf("derived preset = %v", tp.Preset)
			}
			if tp.Verification != verify {
				t.Fatalf("derived verify = %v, want %v", tp.Verification, verify)
			}
		})
	}
}

func TestLegacyTokenModeDualWriteRoundTrip(t *testing.T) {
	pairs := []struct {
		legacy string
		preset agentpreset.AgentPreset
	}{
		{"economy", agentpreset.Balanced},
		{"full", agentpreset.Balanced},
		{"delivery", agentpreset.Delivery},
		{"light", agentpreset.Balanced},
		{"balanced", agentpreset.Balanced},
	}
	for _, p := range pairs {
		got := agentpreset.Normalize(p.legacy)
		if got != p.preset {
			t.Fatalf("Normalize(%q) = %q, want %q", p.legacy, got, p.preset)
		}
		// dual-write mapping stays stable for one version
		legacy := agentpreset.LegacyTokenMode(got)
		round := agentpreset.FromLegacyTokenMode(legacy)
		if round != got {
			t.Fatalf("round-trip %q -> %q -> %q", p.legacy, legacy, round)
		}
	}
}
