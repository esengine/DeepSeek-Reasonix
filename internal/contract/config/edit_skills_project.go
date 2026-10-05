package config

// projectSkillsKeysToRemove reports whether an existing project [skills]
// section contains a field whose current edit value is the built-in default.
// Project saves are incremental, so an empty RenderTOMLProjectDelta cannot
// remove a stale override without this explicit cleanup pass.
func projectSkillsKeysToRemove(body string, c *Config) bool {
	if c == nil || !tomlBodyHasSection(body, "skills") {
		return false
	}
	for _, key := range projectSkillKeys {
		if projectSkillKeyIsDefault(c, key) {
			if _, ok := tomlSectionKeyValue(body, "skills", key); ok {
				return true
			}
		}
	}
	return false
}

var projectSkillKeys = [...]string{"paths", "excluded_paths", "disabled_skills", "disable_implicit_invocation", "suppress_warnings", "max_depth"}

func projectSkillKeyIsDefault(c *Config, key string) bool {
	if c != nil && c.keepsProjectSkillKey(key) {
		return false
	}
	switch key {
	case "paths":
		return len(c.Skills.Paths) == 0
	case "excluded_paths":
		return len(c.Skills.ExcludedPaths) == 0
	case "disabled_skills":
		return len(c.Skills.DisabledSkills) == 0
	case "disable_implicit_invocation":
		return !c.Skills.DisableImplicitInvocation
	case "suppress_warnings":
		return !c.Skills.SuppressWarnings
	case "max_depth":
		return c.Skills.MaxDepth == 0
	default:
		return false
	}
}

func cleanupProjectSkillsKeys(body string, c *Config) string {
	if c == nil {
		return body
	}
	for _, key := range projectSkillKeys {
		if projectSkillKeyIsDefault(c, key) {
			body = removeTOMLSectionKey(body, "skills", key)
		}
	}
	return body
}
