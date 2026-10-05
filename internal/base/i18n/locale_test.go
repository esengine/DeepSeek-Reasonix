package i18n

import "testing"

func TestNormalizePOSIXLocales(t *testing.T) {
	for _, tt := range []struct{ locale, want string }{
		{"C", "en"}, {"POSIX", "en"}, {"C.UTF-8", "en"},
		{"C.ISO8859-1", "en"}, {"POSIX.UTF-8", "en"}, {"POSIX.ASCII", "en"},
		{"  c.utf-8  ", "en"}, {"", ""}, {"   ", ""},
		{"zh-Hans-CN", "zh"}, {"zh_TW.UTF-8", "zh-TW"}, {"en-US", "en"},
		{"ca-ES", ""}, {"posix-extra", ""},
	} {
		t.Run(tt.locale, func(t *testing.T) {
			if got := normalize(tt.locale); got != tt.want {
				t.Fatalf("normalize(%q) = %q, want %q", tt.locale, got, tt.want)
			}
		})
	}
}

func TestPOSIXLocaleWinsOverSystemLanguage(t *testing.T) {
	before, probe := CurrentLanguage(), osLanguage
	osLanguage = func() string { return "zh-TW" }
	t.Cleanup(func() { setLanguage(before); osLanguage = probe })
	for _, key := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		for _, locale := range []string{"C", "POSIX", "C.UTF-8", "C.ASCII", "POSIX.UTF-8"} {
			t.Run(key+"/"+locale, func(t *testing.T) {
				for _, candidate := range []string{"REASONIX_LANG", "LC_ALL", "LC_MESSAGES", "LANG"} {
					t.Setenv(candidate, "")
				}
				t.Setenv("REASONIX_SYSTEM_LANG", "zh-Hans-CN")
				t.Setenv(key, locale)
				if got := DetectLanguage("auto"); got != "en" {
					t.Fatalf("language = %q, want en", got)
				}
				if got := CatalogFor("auto").TrayOpen; got != English.TrayOpen {
					t.Fatalf("tray open = %q, want %q", got, English.TrayOpen)
				}
				t.Setenv("REASONIX_LANG", "zh-TW")
				if got := DetectLanguage("auto"); got != "zh-TW" {
					t.Fatalf("environment override = %q, want zh-TW", got)
				}
				if got := DetectLanguage("zh"); got != "zh" {
					t.Fatalf("explicit override = %q, want zh", got)
				}
			})
		}
	}
}
