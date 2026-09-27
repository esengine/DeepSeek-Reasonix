package boot

import (
	"errors"
	"os"
	"strings"
	"unicode/utf8"
)

func appendExternalSystemPrompt(base, path string) (string, error) {
	if path == "" {
		return base, nil
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return "", errors.New("append system prompt file must be a readable regular file")
	}
	content, err := os.ReadFile(path)
	if err != nil {
		return "", errors.New("append system prompt file could not be read")
	}
	if !utf8.Valid(content) || strings.TrimSpace(string(content)) == "" {
		return "", errors.New("append system prompt file must contain nonempty UTF-8 text")
	}
	if base == "" {
		return string(content), nil
	}
	return strings.TrimRight(base, "\n") + "\n\n" + string(content), nil
}

func externalPromptNeedsRebuild(path string, previous *ReusedAssembly) bool {
	return path != "" || previous != nil && previous.externalSystemPrompt
}

func prepareExternalPromptBuild(opts Options) Options {
	if externalPromptNeedsRebuild(opts.AppendSystemPromptFile, opts.ReuseAssembly) {
		// A graph-only plan cannot observe edits to a process-local prompt file.
		// Recompose the prefix and let its extension owner replace it again.
		opts.ReuseAssembly = nil
		opts.PreviousPlan = nil
	}
	return opts
}
