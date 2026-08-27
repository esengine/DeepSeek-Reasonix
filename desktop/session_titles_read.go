package main

import (
	"encoding/json"
	"errors"
	"os"

	"reasonix/internal/agent"
)

// loadSessionTitles reads the basename→title map (missing/corrupt → empty).
func loadSessionTitles(dir string) map[string]string {
	m, err := loadSessionTitlesWithError(dir)
	if err != nil {
		return map[string]string{}
	}
	return m
}

// loadSessionTitlesWithError preserves sidecar read and decode failures for
// migrations that must not certify a fallback while a custom title may still
// be recoverable. A missing sidecar is the ordinary no-overrides case.
func loadSessionTitlesWithError(dir string) (map[string]string, error) {
	m := map[string]string{}
	b, err := readFileWithTimeout(sessionTitlesPath(dir), topicFileReadTimeout)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return m, nil
		}
		return nil, err
	}
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	if m == nil {
		m = map[string]string{}
	}
	// Older builds could persist titles polluted with internal wrappers
	// (memory-compiler contracts, transient blocks) — clean at the read
	// boundary; UserPreviewText is a no-op on clean titles (#5666).
	for key, title := range m {
		m[key] = agent.UserPreviewText(title)
	}
	return m, nil
}
