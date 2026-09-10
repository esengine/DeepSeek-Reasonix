package browser

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"reasonix/internal/provider"
)

func TestSchemasAreCanonicalAndClosed(t *testing.T) {
	first := Tools(&fakeExecutor{})
	second := Tools(&fakeExecutor{})
	for i, tl := range first {
		name := tl.Name()
		raw := tl.Schema()
		if !json.Valid(raw) {
			t.Errorf("%s schema is invalid JSON: %s", name, raw)
			continue
		}
		if got := string(provider.CanonicalizeSchema(raw)); got != string(raw) {
			t.Errorf("%s schema is not canonical:\n got %s\nwant %s", name, raw, got)
		}
		if string(tl.Schema()) != string(raw) || string(second[i].Schema()) != string(raw) {
			t.Errorf("%s schema is not deterministic", name)
		}
		var s struct {
			Type                 string                     `json:"type"`
			AdditionalProperties *bool                      `json:"additionalProperties"`
			Properties           map[string]json.RawMessage `json:"properties"`
			Required             *[]string                  `json:"required"`
		}
		if err := json.Unmarshal(raw, &s); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if s.Type != "object" || s.AdditionalProperties == nil || *s.AdditionalProperties {
			t.Errorf("%s schema is not a closed object: %s", name, raw)
		}
		if s.Required == nil {
			t.Errorf("%s schema declares no required list", name)
			continue
		}
		if len(*s.Required) == 0 && name != "browser_tabs" {
			t.Errorf("%s schema has an empty required list", name)
		}
		if !slices.IsSorted(*s.Required) {
			t.Errorf("%s required is not sorted: %v", name, *s.Required)
		}
		for _, r := range *s.Required {
			if _, ok := s.Properties[r]; !ok {
				t.Errorf("%s requires undeclared property %q", name, r)
			}
		}
		if readOnlyTools[name] {
			if _, ok := s.Properties["operationId"]; ok {
				t.Errorf("%s is read-only but declares operationId", name)
			}
			continue
		}
		if !slices.Contains(*s.Required, "operationId") {
			t.Errorf("%s is a write but does not require operationId: %v", name, *s.Required)
		}
	}
}

func TestSchemaTeachesWorkflow(t *testing.T) {
	click := string(toolByName(t, nil, "browser_click").Schema())
	if !strings.Contains(click, `"pattern":"^[A-Za-z0-9_-]{1,100}$"`) {
		t.Errorf("browser_click operationId lacks the pattern: %s", click)
	}
	for _, want := range []string{"never reuse", "documentToken", "browser_snapshot"} {
		if !strings.Contains(click, want) {
			t.Errorf("browser_click schema does not mention %q", want)
		}
	}
	nav := string(toolByName(t, nil, "browser_navigate").Schema())
	if !strings.Contains(nav, `"enum":["url","back","forward","reload"]`) {
		t.Errorf("browser_navigate action enum missing: %s", nav)
	}
	if got := string(toolByName(t, nil, "browser_tabs").Schema()); got != `{"additionalProperties":false,"properties":{},"required":[],"type":"object"}` {
		t.Errorf("browser_tabs schema = %s", got)
	}
}
