package agentpreset_test

import (
	"testing"

	"reasonix/internal/agentpreset"
	"reasonix/internal/taskpolicy"
)

// TestRoleSettingMatrix locks the product contract for the three role settings:
// shared tool surface is owned by boot; this package owns planner/review floors.
func TestRoleSettingMatrix(t *testing.T) {
	type want struct {
		semanticRouter bool
		atomic         bool
		verify         agentpreset.VerificationLevel
		mediumReview   agentpreset.ReviewLevel
		explore        bool
	}
	cases := map[agentpreset.AgentPreset]want{
		agentpreset.Light: {
			semanticRouter: false,
			atomic:         false,
			verify:         agentpreset.VerifyTargeted,
			mediumReview:   agentpreset.ReviewNone,
			explore:        false,
		},
		agentpreset.Balanced: {
			semanticRouter: true,
			atomic:         false,
			verify:         agentpreset.VerifyTargeted,
			mediumReview:   agentpreset.ReviewConditional,
			explore:        true,
		},
		agentpreset.Delivery: {
			semanticRouter: true,
			atomic:         true,
			verify:         agentpreset.VerifyFull,
			mediumReview:   agentpreset.ReviewForced,
			explore:        true,
		},
	}
	for preset, w := range cases {
		t.Run(string(preset), func(t *testing.T) {
			p := agentpreset.PolicyOf(preset)
			if p.CapabilityPolicy.SemanticRouterAllowed != w.semanticRouter {
				t.Fatalf("semantic router = %v, want %v", p.CapabilityPolicy.SemanticRouterAllowed, w.semanticRouter)
			}
			if p.PlannerPolicy.RequireAtomicContract != w.atomic {
				t.Fatalf("atomic contract = %v, want %v", p.PlannerPolicy.RequireAtomicContract, w.atomic)
			}
			if p.VerificationPolicy.Level != w.verify {
				t.Fatalf("verify = %v, want %v", p.VerificationPolicy.Level, w.verify)
			}
			if p.ReviewPolicy.MediumRisk != w.mediumReview {
				t.Fatalf("medium review = %v, want %v", p.ReviewPolicy.MediumRisk, w.mediumReview)
			}
			if p.PlannerPolicy.AllowExploreSubagent != w.explore {
				t.Fatalf("explore = %v, want %v", p.PlannerPolicy.AllowExploreSubagent, w.explore)
			}
			// Derive a low-risk conversational turn: never forces full plan/review.
			tp := taskpolicy.Derive(taskpolicy.Input{
				Raw:    "hello",
				Preset: preset,
			})
			if tp.Preset != preset {
				t.Fatalf("derived preset = %v", tp.Preset)
			}
			if tp.Route != taskpolicy.RouteDirect && preset != agentpreset.Delivery {
				// light/balanced greetings stay direct
				t.Fatalf("conversation route = %v, want direct for %s", tp.Route, preset)
			}
		})
	}
}

func TestLegacyTokenModeDualWriteRoundTrip(t *testing.T) {
	pairs := []struct {
		legacy string
		preset agentpreset.AgentPreset
	}{
		{"economy", agentpreset.Light},
		{"full", agentpreset.Balanced},
		{"delivery", agentpreset.Delivery},
		{"light", agentpreset.Light},
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
