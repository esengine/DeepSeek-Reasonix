package doctor

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"reasonix/internal/skill"
	"reasonix/internal/tool"
)

type registryProbeTool struct{}

func (registryProbeTool) Name() string        { return "doctor_registry_probe" }
func (registryProbeTool) Description() string { return "probe" }
func (registryProbeTool) Schema() json.RawMessage {
	return json.RawMessage(`{"type":"object"}`)
}
func (registryProbeTool) ReadOnly() bool { return true }
func (registryProbeTool) Execute(context.Context, json.RawMessage) (string, error) {
	return "", nil
}

// A skill may name any registered built-in in allowed-tools. The hand-written
// list in isBuiltinOrMetaTool had drifted from the tools that actually ship —
// use_capability, compress and update_goal were all missing, so every session
// carrying a skill that names one reported a warning for a tool that exists.
// Consult the registry, which cannot drift, before falling back to the list.
func TestAllowedToolsAcceptsAnyRegisteredBuiltin(t *testing.T) {
	tool.RegisterBuiltin(registryProbeTool{})

	warns := CollectSkillHealthWarnings(SkillHealthOptions{
		Skills: []skill.Skill{{
			Name:         "probe",
			Description:  "ok",
			AllowedTools: []string{"doctor_registry_probe"},
		}},
	})
	for _, w := range warns {
		if strings.Contains(w, "doctor_registry_probe") {
			t.Fatalf("registered built-in reported as missing: %s", w)
		}
	}
}

// The complement: a name that is neither registered nor a known meta tool must
// still warn, so the softened check does not silence real typos.
func TestAllowedToolsStillWarnsOnAnUnknownName(t *testing.T) {
	warns := CollectSkillHealthWarnings(SkillHealthOptions{
		Skills: []skill.Skill{{
			Name:         "probe",
			Description:  "ok",
			AllowedTools: []string{"definitely_not_a_tool"},
		}},
	})
	for _, w := range warns {
		if strings.Contains(w, "definitely_not_a_tool") {
			return
		}
	}
	t.Fatal("unknown allowed-tools name did not warn")
}
