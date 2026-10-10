package pluginpkg

import (
	"fmt"
	"slices"
	"strings"
)

func (p Package) agentRefs() []AgentRef {
	refs, _ := p.agentRefsWithWarnings()
	return refs
}

func (p Package) agentNameWarnings() []string {
	_, warnings := p.agentRefsWithWarnings()
	return warnings
}

func (p Package) selectAgentRefs(refs []AgentRef) ([]AgentRef, []string) {
	byName := map[string]string{}
	seenPaths := map[string]bool{}
	var warnings []string
	filtered := refs[:0]
	for _, ref := range refs {
		if seenPaths[ref.Path] {
			continue
		}
		seenPaths[ref.Path] = true
		if previous, exists := byName[ref.Name]; exists {
			warnings = append(warnings, fmt.Sprintf("agent name %q uses %s; %s is shadowed", ref.Name, RelativeRoot(p.Root, previous), RelativeRoot(p.Root, ref.Path)))
			continue
		}
		byName[ref.Name] = ref.Path
		filtered = append(filtered, ref)
	}
	slices.SortStableFunc(filtered, func(a, b AgentRef) int { return strings.Compare(a.Name, b.Name) })
	return filtered, warnings
}
