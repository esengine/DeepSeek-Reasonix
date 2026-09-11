package config

import "strings"

// ConfigFileDefinesCompactRatio reports whether path explicitly overrides the
// automatic compaction threshold. It is used by config surfaces that need to
// explain whether the effective value came from defaults, user config, or the
// current project.
func ConfigFileDefinesCompactRatio(path string) bool {
	return tomlFileDefinesKey(path, "agent", "compact_ratio")
}

// ConfigFileDefinesSkillKey reports whether a project or user TOML file
// explicitly owns one of the supported [skills] settings. Desktop settings use
// this narrow provenance check to edit the file that wins at runtime instead
// of persisting a shadowed value to the global config.
func ConfigFileDefinesSkillKey(path, key string) bool {
	switch strings.TrimSpace(key) {
	case "paths", "excluded_paths", "disabled_skills", "disable_implicit_invocation", "max_depth":
		return tomlFileDefinesKey(path, "skills", key)
	default:
		return false
	}
}

// ConfigFileDefinesWebSearchModel reports an explicit project assignment override.
func ConfigFileDefinesWebSearchModel(path string) bool {
	return tomlFileDefinesKey(path, "agent", "web_search_model")
}
