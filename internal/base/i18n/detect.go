package i18n

import (
	"os"
	"strings"
)

// DetectLanguage installs the catalogue for an explicit setting or locale.
// Explicit settings and locale variables take priority over the shell's system
// language and the native OS probe; unresolved locales fall back to English.
func DetectLanguage(override string) string {
	for _, c := range append([]string{override}, envCandidates()...) {
		if tag := normalize(c); tag != "" {
			return setLanguage(tag)
		}
	}
	return setLanguage("en")
}

// osLanguage is the machine probe behind a variable, so the priority between it
// and the environment is testable on a machine that answers either way.
var osLanguage = detectOSLanguage

func envCandidates() []string {
	keys := []string{"REASONIX_LANG", "LC_ALL", "LC_MESSAGES", "LANG", "REASONIX_SYSTEM_LANG"}
	out := make([]string, 0, len(keys)+1)
	for _, k := range keys {
		out = append(out, os.Getenv(k))
	}
	// Last, so an explicit variable still wins: it is what someone set on
	// purpose, and this is only what the machine was installed as.
	return append(out, osLanguage())
}

func setLanguage(tag string) string {
	switch tag {
	case "zh-tw", "zh-TW":
		M = ChineseTraditional
		currentLanguage = "zh-TW"
	case "zh":
		M = Chinese
		currentLanguage = "zh"
	default:
		M = English
		currentLanguage = "en"
	}
	return currentLanguage
}

// normalize maps a locale string (e.g. "zh_CN.UTF-8", "zh-Hans-CN", "Chinese
// (China)") to a short tag this package knows about. Returns "" for empty or
// unrecognised input so DetectLanguage can fall through to the next candidate.
func normalize(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, "_", "-") // zh_TW.UTF-8 → zh-tw.utf-8 (POSIX locales use underscores)
	if s == "" {
		return ""
	}
	if s == "c" || s == "posix" || strings.HasPrefix(s, "c.") || strings.HasPrefix(s, "posix.") {
		return "en"
	}
	if strings.HasPrefix(s, "zh-tw") || strings.HasPrefix(s, "zh-hant") || strings.Contains(s, "chinese traditional") || strings.Contains(s, "繁體") {
		return "zh-TW"
	}
	if strings.HasPrefix(s, "zh") || strings.Contains(s, "chinese") || strings.Contains(s, "中文") {
		return "zh"
	}
	if strings.HasPrefix(s, "en") || strings.Contains(s, "english") {
		return "en"
	}
	return ""
}
