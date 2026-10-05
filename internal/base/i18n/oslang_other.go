//go:build !windows

package i18n

// detectOSLanguage is the Windows-only probe. Everywhere else the locale variables
// above are the answer, and asking twice would only add a second one.
func detectOSLanguage() string { return "" }
