package config

import (
	"maps"
	"testing"
)

func TestProjectRoleEffortsAreUserOnly(t *testing.T) {
	user := `[agent]
role_efforts = { planner = "low", subagent = "low", guardian = "low", vision = "low", title = "low" }
`
	want := map[string]string{"planner": "low", "subagent": "low", "guardian": "low", "vision": "low", "title": "low"}
	for _, tc := range []struct {
		name    string
		user    string
		project string
		want    map[string]string
		ignored bool
	}{
		{"cannot override user choices", user, `[agent]
role_efforts = { planner = "max", subagent = "max", guardian = "max", title = "high" }
`, want, true},
		{"cannot supply missing user choices", "", `[agent]
role_efforts = { planner = "max", subagent = "max", guardian = "max", title = "high" }
`, nil, true},
		{"cannot lower user choices", `[agent]
role_efforts = { planner = "high" }
`, `[agent]
role_efforts = { planner = "low" }
`, map[string]string{"planner": "high"}, true},
		{"user choices without project efforts", user, "", want, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg, _ := loadScoped(t, tc.user, tc.project)
			if !maps.Equal(cfg.Agent.RoleEfforts, tc.want) {
				t.Fatalf("role_efforts = %v, want user choices %v", cfg.Agent.RoleEfforts, tc.want)
			}
			ignored := false
			for _, setting := range cfg.IgnoredProjectSettings() {
				if setting.Key == "agent.role_efforts" && setting.Reason == ProjectUserOnly {
					ignored = true
				}
			}
			if ignored != tc.ignored {
				t.Fatalf("user-only refusal = %t, want %t; ignored settings: %v", ignored, tc.ignored, cfg.IgnoredProjectSettings())
			}
		})
	}
}
