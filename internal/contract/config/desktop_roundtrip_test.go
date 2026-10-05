package config

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// The [desktop] table is written by hand, one line per field, so a field added
// to the struct without a line in the renderer is dropped on every save and
// reads back as a setting the window forgot. Each exemption below names why
// that field is deliberately not written.
var desktopNotWritten = map[string]string{
	"update_channel":  "legacy: read so an older config still resolves, never written back",
	"expand_thinking": "deprecated alias for reasoning_display_mode, which is what gets written",
	"currency":        "legacy: written only when already set, superseded by [billing].display_currency",
}

// A value for each kind that is distinguishable from the zero one, so a field
// that vanished and a field that was never set cannot be confused.
func markDesktopField(t *testing.T, f reflect.Value, name string) bool {
	t.Helper()
	switch f.Kind() {
	case reflect.String:
		if v, ok := desktopStringMark[name]; ok {
			f.SetString(v)
			return true
		}
		return false
	case reflect.Bool:
		f.SetBool(true)
		return true
	case reflect.Pointer:
		if f.Type().Elem().Kind() != reflect.Bool {
			return false
		}
		no := false
		f.Set(reflect.ValueOf(&no))
		return true
	case reflect.Slice:
		if f.Type().Elem().Kind() != reflect.String {
			return false
		}
		if v, ok := desktopListMark[name]; ok {
			f.Set(reflect.ValueOf(v))
			return true
		}
		return false
	case reflect.Map:
		if f.Type().Elem().Kind() != reflect.String {
			return false
		}
		f.Set(reflect.ValueOf(map[string]string{"round:trip": "composer"}))
		return true
	}
	return false
}

// Every marker is a value its own field accepts. A value the loader normalises
// away comes back as the default, which is indistinguishable from the field
// having been dropped — the test would then report the wrong defect.
var desktopListMark = map[string][]string{
	"status_bar_items": {"cost", "context"},
	"provider_access":  {"deepseek", "moonshot"},
}
var desktopStringMark = map[string]string{
	"language":                   "en",
	"theme":                      "light",
	"theme_style":                "aurora",
	"theme_pack":                 "round-trip-pack",
	"terminal_theme":             "light",
	"external_opener":            "round-trip-opener",
	"close_behavior":             "background",
	"tray":                       "off",
	"display_mode":               "compact",
	"status_bar_style":           "text",
	"default_tool_approval_mode": "yolo",
	"reasoning_display_mode":     "hidden",
	"conversation_width":         "full",
	"pinned_version":             "9.9.9",
	"editor":                     "/usr/local/bin/round-trip-editor",
}

func TestEveryDesktopSettingSurvivesASave(t *testing.T) {
	typ := reflect.TypeFor[DesktopConfig]()
	var lost []string
	for i := range typ.NumField() {
		field := typ.Field(i)
		name := strings.Split(field.Tag.Get("toml"), ",")[0]
		if name == "" || name == "appearance" {
			continue
		}
		if why, ok := desktopNotWritten[name]; ok {
			if why == "" {
				t.Errorf("%s is exempted with no reason", name)
			}
			continue
		}

		cfg := Default()
		set := reflect.ValueOf(&cfg.Desktop).Elem().Field(i)
		if !markDesktopField(t, set, name) {
			t.Errorf("%s (%s) has no round-trip value; add one so it is covered rather than skipped", name, field.Type)
			continue
		}
		want := set.Interface()

		path := filepath.Join(t.TempDir(), "config.toml")
		if err := os.WriteFile(path, []byte(RenderTOMLForScope(cfg, RenderScopeUser)), 0o600); err != nil {
			t.Fatal(err)
		}
		back := LoadForEdit(path)
		got := reflect.ValueOf(&back.Desktop).Elem().Field(i).Interface()
		if !reflect.DeepEqual(got, want) {
			lost = append(lost, name+": saved "+render(want)+", read back "+render(got))
		}
	}
	if len(lost) > 0 {
		t.Fatalf("settings the window forgets on the next launch:\n  %s", strings.Join(lost, "\n  "))
	}
}

func render(v any) string { return fmt.Sprintf("%v", v) }
