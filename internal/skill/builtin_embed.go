package skill

import (
	"fmt"
	"sync"

	"reasonix/internal/skill/builtincontent"
)

var (
	embeddedOnce   sync.Once
	embeddedSkills []Skill
	embeddedErr    error
)

// loadEmbeddedBuiltins returns skills compiled into the binary via go:embed.
// Failures are sticky for the process so a corrupt embed fails loudly once.
func loadEmbeddedBuiltins() []Skill {
	embeddedOnce.Do(func() {
		items, err := builtincontent.All()
		if err != nil {
			embeddedErr = err
			return
		}
		out := make([]Skill, 0, len(items))
		for _, item := range items {
			out = append(out, skillFromEmbedded(item))
		}
		embeddedSkills = out
	})
	if embeddedErr != nil {
		// Panic in development/tests; production binaries always ship valid embeds.
		panic(fmt.Sprintf("embedded builtin skills: %v", embeddedErr))
	}
	return append([]Skill(nil), embeddedSkills...)
}

func skillFromEmbedded(item builtincontent.SkillMarkdown) Skill {
	return Skill{
		Name:        item.Name,
		Description: item.Description,
		Body:        item.Body,
		Scope:       ScopeBuiltin,
		Path:        "(builtin:" + item.Path + ")",
		RunAs:       parseRunAs(item.RunAs, item.Context, item.Agent),
	}
}
