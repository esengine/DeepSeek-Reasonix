package i18n

import (
	"maps"
	"reflect"
	"slices"
	"strings"
	"testing"
)

func TestFeedbackTextIsCompleteInEveryCatalogue(t *testing.T) {
	en := reflect.ValueOf(English.Feedback)
	for tag, cat := range map[string]FeedbackText{"zh": Chinese.Feedback, "zh-TW": ChineseTraditional.Feedback} {
		got := reflect.ValueOf(cat)
		for i := range en.NumField() {
			name := en.Type().Field(i).Name
			switch en.Field(i).Kind() {
			case reflect.String:
				if strings.TrimSpace(got.Field(i).String()) == "" {
					t.Errorf("%s: %s is empty", tag, name)
				}
				if strings.HasSuffix(name, "Fmt") && countVerbs(en.Field(i).String()) != countVerbs(got.Field(i).String()) {
					t.Errorf("%s: %s has a different number of verbs than en", tag, name)
				}
			case reflect.Map:
				want := slices.Sorted(maps.Keys(en.Field(i).Interface().(map[string]string)))
				have := slices.Sorted(maps.Keys(got.Field(i).Interface().(map[string]string)))
				if !slices.Equal(want, have) {
					t.Errorf("%s: %s keys = %v, want %v", tag, name, have, want)
				}
				for k, v := range got.Field(i).Interface().(map[string]string) {
					if strings.TrimSpace(v) == "" {
						t.Errorf("%s: %s[%s] is empty", tag, name, k)
					}
				}
			}
		}
	}
}

func TestFeedbackLevelNamesCoverEveryLevelTheServiceDefines(t *testing.T) {
	for _, m := range []Messages{English, Chinese, ChineseTraditional} {
		for level := range 7 {
			if m.Feedback.LevelNames[string(rune('0'+level))] == "" {
				t.Errorf("no name for level %d", level)
			}
		}
	}
}
