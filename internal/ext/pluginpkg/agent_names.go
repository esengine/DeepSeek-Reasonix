package pluginpkg

import "fmt"

func (p Package) validateAgentNames(warnings []string) (Package, []string, error) {
	byName := map[string]string{}
	for _, ref := range p.agentRefs() {
		if previous, exists := byName[ref.Name]; exists && previous != ref.Path {
			return Package{}, warnings, fmt.Errorf("agent name %q is declared by both %s and %s", ref.Name, RelativeRoot(p.Root, previous), RelativeRoot(p.Root, ref.Path))
		}
		byName[ref.Name] = ref.Path
	}
	return p, warnings, nil
}
