package config

import (
	"path/filepath"
	"reflect"
	"testing"

	"reasonix/internal/base/testenv"
)

// Keys the loader deliberately drops: retired knobs kept only so an old TOML
// still parses. They are asserted NOT to survive, which is the opposite claim
// and just as strong — the list cannot quietly become a place to park a field
// the renderer merely forgot.
var retiredAgentKeys = map[string]bool{
	"max_steps":              true,
	"planner_max_steps":      true,
	"soft_compact_ratio":     true,
	"compact_force_ratio":    true,
	"tool_result_snip_ratio": true,
	"cold_resume_prune":      true,
	"context_editing":        true,
	"recovery_temperature":   true,
	"auto_plan":              true,
	"auto_plan_classifier":   true,
}

// probe fills a field with a value distinguishable from its zero, so a setting
// that silently fails to persist reads back as the zero it started from.
func probe(f reflect.Value, key string) bool {
	switch f.Kind() {
	case reflect.String:
		switch key {
		case "reasoning_language":
			f.SetString("en")
		case "auto_plan":
			f.SetString("on")
		default:
			f.SetString("probe-" + key)
		}
	case reflect.Int, reflect.Int64:
		f.SetInt(7)
	case reflect.Float64:
		f.SetFloat(0.25)
	case reflect.Bool:
		f.SetBool(true)
	default:
		return false // maps, slices and pointers carry their own shapes
	}
	return true
}

// Every [agent] setting the kernel still reads has to survive being written and
// loaded back. It is the renderer that decides: config writing is hand-rolled
// TOML, so a field added to the struct without a line in RenderTOML is accepted
// by the API, saved without error, and gone on the next read — which is exactly
// how guardian_model became a setting that could not be set.
func TestEveryLiveAgentSettingSurvivesARoundTrip(t *testing.T) {
	dir := testenv.TempDir(t)
	t.Setenv("REASONIX_HOME", dir)
	path := filepath.Join(dir, "config.toml")

	fields := reflect.VisibleFields(reflect.TypeFor[AgentConfig]())
	for _, sf := range fields {
		key := sf.Tag.Get("toml")
		if key == "" || key == "-" {
			continue
		}
		t.Run(key, func(t *testing.T) {
			edit := LoadForEdit(path)
			set := reflect.ValueOf(&edit.Agent).Elem().FieldByName(sf.Name)
			if !probe(set, key) {
				t.Skipf("%s is not a scalar setting", key)
			}
			want := set.Interface()
			if err := edit.SaveTo(path); err != nil {
				t.Fatal(err)
			}

			back := LoadForEdit(path)
			got := reflect.ValueOf(&back.Agent).Elem().FieldByName(sf.Name).Interface()
			if retiredAgentKeys[key] {
				if reflect.DeepEqual(got, want) {
					t.Errorf("%s is listed as retired but the value stuck (%v) — drop it from the list", key, got)
				}
				return
			}
			if !reflect.DeepEqual(got, want) {
				t.Errorf("%s = %v after a save and load, want %v — the renderer has no line for it", key, got, want)
			}
		})
	}
}
