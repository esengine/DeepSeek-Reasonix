//go:build windows

package i18n

import "golang.org/x/sys/windows"

// detectOSLanguage asks Windows directly. It sets none of the POSIX locale variables
// the env candidates read, so every native surface on a Chinese Windows fell
// back to English — the CLI's own words, the folder picker, the status icon's
// menu — while the webview beside them read the browser's list and got it right.
func detectOSLanguage() string {
	langs, err := windows.GetUserPreferredUILanguages(windows.MUI_LANGUAGE_NAME)
	if err != nil || len(langs) == 0 {
		return ""
	}
	return langs[0]
}
