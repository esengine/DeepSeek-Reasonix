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

type skillScan struct {
	refs     []SkillRef
	warnings []string
}

func (p Package) skillRefsWithWarnings() ([]SkillRef, []string) {
	if p.skills != nil {
		return p.skills.refs, p.skills.warnings
	}
	s := p.scanSkills()
	return s.refs, s.warnings
}

func (p Package) scanSkills() *skillScan {
	refs, warnings := p.walkSkills()
	return &skillScan{refs: refs, warnings: warnings}
}

func (p Package) walkSkills() ([]SkillRef, []string) {
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
