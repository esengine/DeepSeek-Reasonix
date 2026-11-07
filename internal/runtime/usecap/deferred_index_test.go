package usecap_test

import (
	"strings"
	"testing"

	"reasonix/internal/contract/tool"
	"reasonix/internal/runtime/usecap"
)

type describedTool struct {
	fakeRegistryTool
	desc string
}

func (t describedTool) Description() string { return t.desc }

func indexRegistry() *tool.Registry {
	reg := tool.NewRegistry()
	reg.Add(describedTool{fakeRegistryTool{name: "web_fetch", readOnly: true}, "Read HTTP(S) text. Reduces HTML to text."})
	reg.Add(describedTool{fakeRegistryTool{name: "browser_open", readOnly: true}, "Load a page and return its snapshot, one element per line."})
	reg.Add(describedTool{fakeRegistryTool{name: "grep", readOnly: true}, "Search file contents."})
	reg.Add(describedTool{fakeRegistryTool{name: "read_file", readOnly: true}, "Read a file."})
	reg.SetProviderVisibleTools([]string{"read_file"})
	return reg
}

func TestDescriptionNamesTheDeferredToolsAModelWouldOtherwiseNotKnowAbout(t *testing.T) {
	desc := usecap.NewUseCapabilityTool(t.Context(), nil, nil, indexRegistry(), nil, nil, nil).Description()
	for _, want := range []string{"tool:web_fetch", "Read HTTP(S) text.", "tool:browser_open", "tool:grep"} {
		if !strings.Contains(desc, want) {
			t.Errorf("description does not carry %q:\n%s", want, desc)
		}
	}
	if strings.Contains(desc, "tool:read_file") {
		t.Errorf("a tool the provider already sees is listed again:\n%s", desc)
	}
}

func TestDescriptionIndexIsEmptyWhenNothingIsDeferred(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(describedTool{fakeRegistryTool{name: "web_fetch", readOnly: true}, "Read HTTP(S) text."})
	desc := usecap.NewUseCapabilityTool(t.Context(), nil, nil, reg, nil, nil, nil).Description()
	if strings.Contains(desc, "tool:web_fetch") {
		t.Errorf("a registry that shows everything still got an index:\n%s", desc)
	}
	if bare := usecap.NewUseCapabilityTool(t.Context(), nil, nil, nil, nil, nil, nil).Description(); bare == "" {
		t.Error("no registry left the description empty")
	}
}

func TestDescriptionIndexIsByteStable(t *testing.T) {
	a := usecap.NewUseCapabilityTool(t.Context(), nil, nil, indexRegistry(), nil, nil, nil).Description()
	b := usecap.NewUseCapabilityTool(t.Context(), nil, nil, indexRegistry(), nil, nil, nil).Description()
	if a != b {
		t.Fatalf("the same registry produced two descriptions:\n%s\n%s", a, b)
	}
}

func TestDescriptionIndexEndsARowBeforeAParenthetical(t *testing.T) {
	reg := tool.NewRegistry()
	reg.Add(describedTool{fakeRegistryTool{name: "glob", readOnly: true}, "Find files matching a glob pattern (e.g. *.go). More text."})
	reg.Add(describedTool{fakeRegistryTool{name: "read_file", readOnly: true}, "Read."})
	reg.SetProviderVisibleTools([]string{"read_file"})
	desc := usecap.NewUseCapabilityTool(t.Context(), nil, nil, reg, nil, nil, nil).Description()
	if !strings.Contains(desc, "tool:glob (Find files matching a glob pattern)") {
		t.Errorf("row not cut before the parenthetical:\n%s", desc)
	}
}

func TestDescriptionOfANilToolIsTheBase(t *testing.T) {
	var none *usecap.UseCapabilityTool
	if none.Description() == "" {
		t.Fatal("a nil tool has no description")
	}
}
