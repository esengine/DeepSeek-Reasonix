package main

import (
	"testing"

	"reasonix/internal/i18n"
)

func TestRefreshBackendNoticeLocale(t *testing.T) {
	i18n.DetectLanguage("zh")
	t.Cleanup(func() { i18n.DetectLanguage("en") })

	refreshBackendNoticeLocale("en")
	if got := i18n.CurrentLanguage(); got != "en" {
		t.Fatalf("backend catalog language = %q, want en", got)
	}

	refreshBackendNoticeLocale("zh")
	if got := i18n.CurrentLanguage(); got != "zh" {
		t.Fatalf("backend catalog language = %q, want zh", got)
	}
}
