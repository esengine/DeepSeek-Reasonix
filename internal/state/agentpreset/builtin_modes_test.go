package agentpreset

import (
	"errors"
	"testing"
)

func TestRetiredBuiltinDefaultRequiresAnExplicitChoice(t *testing.T) {
	s := New(t.TempDir(), t.TempDir())
	if err := writeJSON(s.UserDirectory()+"/settings.json", preferences{Default: "builtin:review"}); err != nil {
		t.Fatal(err)
	}
	catalog, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(catalog.Presets) != 2 || catalog.Default != "builtin:review" || len(catalog.Diagnostics) != 1 {
		t.Fatalf("retired default was silently replaced: %+v", catalog)
	}
	if _, err := s.Get("builtin:review"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("retired built-in remains selectable: %v", err)
	}
	if err := s.SetDefault("builtin:general", false); err != nil {
		t.Fatal(err)
	}
	catalog, err = s.List()
	if err != nil || len(catalog.Diagnostics) != 0 {
		t.Fatalf("explicit replacement failed: %+v, %v", catalog, err)
	}
}
