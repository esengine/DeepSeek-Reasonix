package cli

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/spf13/pflag"
)

func resolveAppendSystemPromptFile(path string) (string, error) {
	invalid := errors.New("external system prompt file must be a readable, nonempty UTF-8 regular file")
	if path == "" {
		return "", invalid
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", invalid
	}
	info, err := os.Stat(absolute)
	if err != nil || !info.Mode().IsRegular() {
		return "", invalid
	}
	content, err := os.ReadFile(absolute)
	if err != nil || !utf8.Valid(content) || strings.TrimSpace(string(content)) == "" {
		return "", invalid
	}
	return absolute, nil
}

func resolveOptionalAppendSystemPromptFile(fs *pflag.FlagSet, path string) (string, error) {
	if !fs.Changed("append-system-prompt-file") {
		return "", nil
	}
	return resolveAppendSystemPromptFile(path)
}
