package cli

import (
	"encoding/json"
	"testing"

	"reasonix/internal/contract/provider"
	"reasonix/internal/ext/skill"
	"reasonix/internal/state/trajectory"
)

func digestFor(caps []trajectory.CapabilityVersion, kind, name string) string {
	for _, c := range caps {
		if c.Kind == kind && c.Name == name {
			return c.Digest
		}
	}
	return ""
}

func TestCapabilityVersionsNameWhatChangedAndNothingElse(t *testing.T) {
	schemas := []provider.ToolSchema{{Name: "read_file", Parameters: json.RawMessage(`{"type":"object"}`)}}
	skills := []skill.Skill{{Name: "review", Description: "review a change", Body: "look closely", Path: "/home/a/.reasonix/skills/review.md"}}
	base, baseSet := capabilityVersions("1.0@abc", "sha256:sys", schemas, skills)

	moved := []skill.Skill{skills[0]}
	moved[0].Path = "/Users/b/.reasonix/skills/review.md"
	if _, set := capabilityVersions("1.0@abc", "sha256:sys", schemas, moved); set != baseSet {
		t.Fatal("the same skill found at another path counted as another version")
	}

	edited := []skill.Skill{skills[0]}
	edited[0].Body = "look closely, twice"
	caps, set := capabilityVersions("1.0@abc", "sha256:sys", schemas, edited)
	if set == baseSet || digestFor(caps, "skill", "review") == digestFor(base, "skill", "review") {
		t.Fatal("editing a skill's body did not change its version")
	}
	if digestFor(caps, "tool", "read_file") != digestFor(base, "tool", "read_file") {
		t.Fatal("editing a skill changed an unrelated tool's version")
	}

	reschema := []provider.ToolSchema{{Name: "read_file", Parameters: json.RawMessage(`{"type":"object","required":["path"]}`)}}
	caps, _ = capabilityVersions("1.0@abc", "sha256:sys", reschema, skills)
	if digestFor(caps, "tool", "read_file") == digestFor(base, "tool", "read_file") {
		t.Fatal("a changed tool schema kept its version")
	}
	if _, set := capabilityVersions("1.1@def", "sha256:sys", schemas, skills); set == baseSet {
		t.Fatal("another host build kept the capability set")
	}
	if base[0].Kind != "host" || base[len(base)-1].Kind != "tool" {
		t.Fatalf("capabilities are not in a stable order: %+v", base)
	}
}
