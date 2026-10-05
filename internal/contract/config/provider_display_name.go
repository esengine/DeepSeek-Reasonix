package config

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// MaxProviderDisplayNameRunes bounds a display name to what a list row can show.
const MaxProviderDisplayNameRunes = 64

var (
	ErrProviderDisplayNameTooLong = errors.New("provider display name is too long")
	ErrProviderDisplayNameInvalid = errors.New("provider display name contains a control character")
)

// Label is what a person calls this entry: its display name when one is set,
// otherwise its name. It is for showing only; refs and lookups use Name.
func (e *ProviderEntry) Label() string {
	if label := strings.TrimSpace(e.DisplayName); label != "" {
		return label
	}
	return e.Name
}

// NormalizeProviderDisplayName trims s and reports whether it may be stored.
// Empty is valid and clears the display name.
func NormalizeProviderDisplayName(s string) (string, error) {
	s = strings.TrimSpace(s)
	if utf8.RuneCountInString(s) > MaxProviderDisplayNameRunes {
		return "", ErrProviderDisplayNameTooLong
	}
	if strings.ContainsFunc(s, unicode.IsControl) {
		return "", ErrProviderDisplayNameInvalid
	}
	return s, nil
}

// SetProviderDisplayName labels every named entry, or none when one is missing
// or the name cannot be stored. Name, and every ref built from it, is untouched.
func (c *Config) SetProviderDisplayName(names []string, displayName string) error {
	label, err := NormalizeProviderDisplayName(displayName)
	if err != nil {
		return err
	}
	targets := make([]*ProviderEntry, 0, len(names))
	for _, name := range names {
		entry, ok := c.Provider(strings.TrimSpace(name))
		if !ok {
			return fmt.Errorf("set provider display name %q: %w", name, ErrProviderNotFound)
		}
		targets = append(targets, entry)
	}
	for _, entry := range targets {
		entry.DisplayName = label
	}
	return nil
}

func renderProviderIdentity(b *strings.Builder, keyEnv, displayName string) {
	fmt.Fprintf(b, "api_key_env = %q\n", keyEnv)
	if displayName = strings.TrimSpace(displayName); displayName != "" {
		fmt.Fprintf(b, "display_name = %q\n", displayName)
	}
}
