package doctor

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"

	"reasonix/internal/skill"
	"reasonix/internal/tool"
)

type registryProbeTool struct{}

func (registryProbeTool) Name() string                                             { return "doctor_registry_probe" }
func (registryProbeTool) Description() string                                      { return "probe" }
func (registryProbeTool) Schema() json.RawMessage                                  { return json.RawMessage(`{"type":"object"}`) }
func (registryProbeTool) ReadOnly() bool                                           { return true }
func (registryProbeTool) Execute(context.Context, json.RawMessage) (string, error) { return "", nil }

// Adapted from PR #9686: isolate registration so repeated and parallel suite
// runs cannot mutate the parent process's compile-time registry.
func TestAllowedToolsAcceptsAnyRegisteredBuiltin(t *testing.T) {
	const marker = "REASONIX_DOCTOR_REGISTRY_PROBE"
	if os.Getenv(marker) != "1" {
		exe, err := os.Executable()
		if err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command(exe, "-test.run=^TestAllowedToolsAcceptsAnyRegisteredBuiltin$")
		cmd.Env = append(os.Environ(), marker+"=1")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("isolated registry check: %v\n%s", err, out)
		}
		return
	}
	tool.RegisterBuiltin(registryProbeTool{})
	warnings := CollectSkillHealthWarnings(SkillHealthOptions{Skills: []skill.Skill{{Name: "probe", Description: "ok", AllowedTools: []string{"doctor_registry_probe"}}}})
	for _, warning := range warnings {
		if strings.Contains(warning, "doctor_registry_probe") {
			t.Fatal(warning)
		}
	}
}

func TestAllowedToolsStillWarnsOnAnUnknownName(t *testing.T) {
	warnings := CollectSkillHealthWarnings(SkillHealthOptions{Skills: []skill.Skill{{Name: "probe", Description: "ok", AllowedTools: []string{"definitely_not_a_tool"}}}})
	for _, warning := range warnings {
		if strings.Contains(warning, "definitely_not_a_tool") {
			return
		}
	}
	t.Fatal("unknown allowed-tools name did not warn")
}
