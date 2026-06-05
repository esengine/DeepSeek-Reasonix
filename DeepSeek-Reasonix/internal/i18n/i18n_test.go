package i18n

import (
	"reflect"
	"strings"
	"testing"
)

// TestCatalogsComplete reflects over English (the baseline) and asserts every
// other catalogue populates the same fields. Empty strings count as missing
// translations so drift fails CI instead of surfacing as blank output. As new
// languages land they get added to the catalogs map below.
func TestCatalogsComplete(t *testing.T) {
	en := reflect.ValueOf(English)
	typ := en.Type()
	catalogs := map[string]reflect.Value{"zh": reflect.ValueOf(Chinese)}
	for tag, cat := range catalogs {
		for i := 0; i < typ.NumField(); i++ {
			name := typ.Field(i).Name
			if strings.TrimSpace(cat.Field(i).String()) == "" {
				t.Errorf("%s catalogue: field %q is empty", tag, name)
			}
		}
	}
}

// TestCatalogsAgreeOnPlaceholders catches translations that silently drop or
// gain %s/%d/%q placeholders — a class of bug that only blows up when the
// affected message is rendered. Compares the count per format verb across
// languages for any field whose name ends in "Fmt".
func TestCatalogsAgreeOnPlaceholders(t *testing.T) {
	en := reflect.ValueOf(English)
	typ := en.Type()
	for i := 0; i < typ.NumField(); i++ {
		name := typ.Field(i).Name
		if !strings.HasSuffix(name, "Fmt") {
			continue
		}
		want := countVerbs(en.Field(i).String())
		got := countVerbs(reflect.ValueOf(Chinese).Field(i).String())
		if want != got {
			t.Errorf("%s: en has %d verbs, zh has %d", name, want, got)
		}
	}
}

// countVerbs counts unescaped fmt placeholders (%s, %d, %q, %v, …). %% does
// not count.
func countVerbs(s string) int {
	n := 0
	for i := 0; i < len(s); i++ {
		if s[i] != '%' {
			continue
		}
		if i+1 < len(s) && s[i+1] == '%' {
			i++
			continue
		}
		n++
	}
	return n
}

// TestNormalize covers the locale-string shapes likely to land in $LANG /
// $LC_ALL / $REASONIX_LANG. Unknown locales return "" so DetectLanguage falls
// through to the next candidate instead of mis-routing.
func TestNormalize(t *testing.T) {
	cases := map[string]string{
		"":                "",
		"en":              "en",
		"en_US.UTF-8":     "en",
		"zh":              "zh",
		"zh_CN.UTF-8":     "zh",
		"zh-Hans-CN":      "zh",
		"Chinese (China)": "zh",
		"中文":              "zh",
		"fr_FR.UTF-8":     "",
		"  ZH_TW  ":       "zh",
	}
	for in, want := range cases {
		if got := normalize(in); got != want {
			t.Errorf("normalize(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestDetectLanguagePriority verifies override beats env and that REASONIX_LANG
// beats LANG. With a clean env we fall back to English.
func TestDetectLanguagePriority(t *testing.T) {
	t.Setenv("REASONIX_LANG", "")
	t.Setenv("LC_ALL", "")
	t.Setenv("LC_MESSAGES", "")
	t.Setenv("LANG", "")
	defer DetectLanguage("") // restore default for other tests

	if got := DetectLanguage(""); got != "en" {
		t.Errorf("clean env: got %q, want en", got)
	}

	t.Setenv("LANG", "zh_CN.UTF-8")
	if got := DetectLanguage(""); got != "zh" {
		t.Errorf("LANG=zh_CN.UTF-8: got %q, want zh", got)
	}

	t.Setenv("REASONIX_LANG", "en")
	if got := DetectLanguage(""); got != "en" {
		t.Errorf("REASONIX_LANG=en overriding LANG=zh: got %q, want en", got)
	}

	if got := DetectLanguage("zh"); got != "zh" {
		t.Errorf("override=zh: got %q, want zh", got)
	}
}
