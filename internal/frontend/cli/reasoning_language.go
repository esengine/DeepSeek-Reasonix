package cli

import (
	"fmt"
	"strings"
)

func parseCLIReasoningLanguage(mode string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "auto":
		return "auto", nil
	case "zh":
		return "zh", nil
	case "en":
		return "en", nil
	default:
		return "", fmt.Errorf("reasoning_language %q: must be auto|zh|en", mode)
	}
}

func cliReasoningLanguageMode(mode string) string {
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case "zh":
		return "zh"
	case "en":
		return "en"
	default:
		return "auto"
	}
}
