package main

import "reasonix/internal/i18n"

// refreshBackendNoticeLocale keeps host-generated notices aligned with the
// live desktop language without adding catalog state to each controller.
func refreshBackendNoticeLocale(lang string) {
	i18n.DetectLanguage(lang)
}
