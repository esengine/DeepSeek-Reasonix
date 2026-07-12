package doctor

import (
	"strings"
	"testing"

	"reasonix/internal/config"
	"reasonix/internal/skill"
)

func TestCollectSkillHealthWarnings(t *testing.T) {
	off := false
	warns := CollectSkillHealthWarnings(SkillHealthOptions{
		Skills: []skill.Skill{
			{Name: "empty", Description: ""},
			{Name: "typo-profile", Description: "ok", InvalidProfiles: []string{"deliverx"}},
			{
				Name:             "conflict",
				Description:      "ok",
				Triggers:         []string{"review"},
				NegativeTriggers: []string{"review"},
				AutoUse:          "require",
				Requires:         []string{"mcp-server:github"},
			},
			{
				Name:        "dup-a",
				Description: "ok",
				Triggers:    []string{"ship it"},
				AutoUse:     "require",
			},
			{
				Name:        "dup-b",
				Description: "ok",
				Triggers:    []string{"ship it"},
				AutoUse:     "require",
			},
		},
		Plugins: []config.PluginEntry{
			{Name: "other", AutoStart: &off},
		},
		FailedServers: map[string]string{"broken": "spawn failed"},
		CacheMismatch: []string{"stale"},
	})
	joined := strings.Join(warns, "\n")
	for _, want := range []string{
		`skill "empty" has a missing or placeholder description`,
		`skill "conflict" trigger "review" also appears in negative-triggers`,
		`skill "conflict" requires mcp-server:github but that MCP server is not configured`,
		`multiple require skills share identical triggers`,
		`MCP server "broken" is in a host-failed state`,
		`MCP server "stale" schema cache fingerprint mismatched`,
		`skill "typo-profile" has illegal profiles value "deliverx"`,
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("warnings missing %q:\n%s", want, joined)
		}
	}
}
