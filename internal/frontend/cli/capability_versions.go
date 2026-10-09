package cli

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"slices"
	"strings"

	"reasonix/internal/contract/provider"
	"reasonix/internal/ext/skill"
	"reasonix/internal/state/trajectory"
)

// capabilityVersions lists what a run could use, by content: the host build
// (which carries the planner, memory and compaction policies compiled into
// it), the system prompt, every tool schema, and every skill. A skill is
// digested by what it says and how it runs, never by where it was found, so
// the same skill on two machines is the same version.
func capabilityVersions(host, systemHash string, schemas []provider.ToolSchema, skills []skill.Skill) ([]trajectory.CapabilityVersion, string) {
	out := []trajectory.CapabilityVersion{
		{Kind: "host", Name: "reasonix", Digest: digestOf(host)},
		{Kind: "prompt", Name: "system", Digest: systemHash},
	}
	for _, s := range schemas {
		b, _ := json.Marshal(s)
		out = append(out, trajectory.CapabilityVersion{Kind: "tool", Name: s.Name, Digest: digestOf(string(b))})
	}
	for _, s := range skills {
		s = s.Complete()
		b, _ := json.Marshal(struct {
			Description, Body, RunAs, Model string
			AllowedTools                    []string
		}{s.Description, s.Body, string(s.RunAs), s.Model, s.AllowedTools})
		out = append(out, trajectory.CapabilityVersion{Kind: "skill", Name: s.Name, Digest: digestOf(string(b))})
	}
	slices.SortFunc(out, func(a, b trajectory.CapabilityVersion) int {
		if c := strings.Compare(a.Kind, b.Kind); c != 0 {
			return c
		}
		return strings.Compare(a.Name, b.Name)
	})
	set, _ := json.Marshal(out)
	return out, digestOf(string(set))
}

func digestOf(s string) string {
	sum := sha256.Sum256([]byte(s))
	return "sha256:" + hex.EncodeToString(sum[:])
}
