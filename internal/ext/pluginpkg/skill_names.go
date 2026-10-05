package pluginpkg

import (
	"cmp"
	"fmt"
	"slices"
)

func (p Package) skillNameWarnings() []string {
	_, warnings := p.skillRefsWithWarnings()
	return warnings
}

func (p Package) skillRefsWithWarnings() ([]SkillRef, []string) {
	var out []SkillRef
	for _, root := range p.SkillRoots() {
		p.scanSkillPath(root, 1, map[string]bool{}, &out)
	}
	byName := map[string]string{}
	seenPaths := map[string]bool{}
	var warnings []string
	filtered := out[:0]
	for _, sk := range out {
		key := sk.Path
		if key == "" {
			key = sk.Name
		}
		if seenPaths[key] {
			continue
		}
		seenPaths[key] = true
		if previous, exists := byName[sk.Name]; exists {
			warnings = append(warnings, fmt.Sprintf("skill name %q uses %s; %s is shadowed", sk.Name, RelativeRoot(p.Root, previous), RelativeRoot(p.Root, sk.Path)))
			continue
		}
		byName[sk.Name] = sk.Path
		filtered = append(filtered, sk)
	}
	slices.SortStableFunc(filtered, func(a, b SkillRef) int {
		return cmp.Or(cmp.Compare(a.Name, b.Name), cmp.Compare(a.Path, b.Path))
	})
	return filtered, warnings
}
