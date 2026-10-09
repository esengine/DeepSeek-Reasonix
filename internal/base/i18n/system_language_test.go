package i18n

import "testing"

func TestShellSystemLanguagePriority(t *testing.T) {
	probe := osLanguage
	osLanguage = func() string { return "en-US" }
	t.Cleanup(func() { osLanguage = probe })
	before := CurrentLanguage()
	t.Cleanup(func() { setLanguage(before) })
	for _, tt := range []struct {
		name, override, key, value, system, want string
	}{
		{"auto", "auto", "", "", "zh-CN", "zh"},
		{"traditional", "auto", "", "", "zh-Hant-TW", "zh-TW"},
		{"setting", "en", "", "", "zh-CN", "en"},
		{"reasonix", "auto", "REASONIX_LANG", "en", "zh-CN", "en"},
		{"all", "auto", "LC_ALL", "en_US.UTF-8", "zh-CN", "en"},
		{"messages", "auto", "LC_MESSAGES", "en_US.UTF-8", "zh-CN", "en"},
		{"lang", "auto", "LANG", "en_US.UTF-8", "zh-CN", "en"},
		{"unsupported", "auto", "", "", "fr-FR", "en"},
		{"absent", "auto", "", "", "", "en"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			for _, key := range []string{"REASONIX_LANG", "LC_ALL", "LC_MESSAGES", "LANG"} {
				t.Setenv(key, "")
			}
			t.Setenv("REASONIX_SYSTEM_LANG", tt.system)
			if tt.key != "" {
				t.Setenv(tt.key, tt.value)
			}
			if got := DetectLanguage(tt.override); got != tt.want {
				t.Fatalf("language = %q, want %q", got, tt.want)
			}
			say := CatalogFor(tt.override)
			want := Catalog(tt.want)
			if say.TrayOpen != want.TrayOpen || say.TrayCloseToTray != want.TrayCloseToTray || say.TrayQuit != want.TrayQuit || say.TrayIdle != want.TrayIdle {
				t.Fatal("tray catalogue does not follow the resolved language")
			}
		})
	}
}
