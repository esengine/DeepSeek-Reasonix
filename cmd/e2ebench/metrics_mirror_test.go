package main

import (
	"reflect"
	"strings"
	"testing"

	"reasonix/internal/cli"
)

// jsonTags collects a struct's JSON field names, descending into embedded and
// nested structs the way encoding/json itself does.
func jsonTags(t reflect.Type, into map[string]bool) {
	for i := range t.NumField() {
		f := t.Field(i)
		tag, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		if tag == "-" {
			continue
		}
		if tag != "" {
			into[tag] = true
		}
		ft := f.Type
		for ft.Kind() == reflect.Pointer {
			ft = ft.Elem()
		}
		if ft.Kind() == reflect.Struct && ft.PkgPath() != "time" {
			jsonTags(ft, into)
		}
	}
}

// The bench reads a metrics file the agent writes, and the two structs are
// hand-mirrored. A tag the bench reads that nobody emits does not fail: the
// field silently stays zero and every report built on it quietly reads as
// "this never happened". Renaming one side must break the build, not the data.
func TestBenchMetricsOnlyReadTagsTheAgentEmits(t *testing.T) {
	emitted := map[string]bool{}
	jsonTags(reflect.TypeFor[cli.RunMetrics](), emitted)

	read := map[string]bool{}
	jsonTags(reflect.TypeFor[runMetrics](), read)

	var orphaned []string
	for tag := range read {
		if !emitted[tag] {
			orphaned = append(orphaned, tag)
		}
	}
	if len(orphaned) > 0 {
		t.Fatalf("e2ebench reads metrics tags no agent writes: %v\n"+
			"either internal/cli.RunMetrics lost them in a rename, or the bench "+
			"invented them; both leave the field zero in every report", orphaned)
	}
}
