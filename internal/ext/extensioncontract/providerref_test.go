package extensioncontract

import "testing"

func TestSplitProviderRefWantsExactlyOneSlash(t *testing.T) {
	for _, ref := range []string{"deepseek/v4", "openai/gpt-5.4"} {
		if _, _, ok := SplitProviderRef(ref); !ok {
			t.Errorf("SplitProviderRef(%q) rejected an ordinary ref", ref)
		}
	}
	for _, ref := range []string{"", "deepseek", "/v4", "deepseek/", "a/b/c"} {
		if _, _, ok := SplitProviderRef(ref); ok {
			t.Errorf("SplitProviderRef(%q) accepted a malformed ref", ref)
		}
	}
	name, model, _ := SplitProviderRef("deepseek/v4")
	if name != "deepseek" || model != "v4" {
		t.Fatalf("SplitProviderRef = %q, %q", name, model)
	}
}

func TestValidProviderTargetAcceptsTheHostedForm(t *testing.T) {
	for _, ref := range []string{"deepseek/v4", "plugin/acme/deepseek/v4"} {
		if !ValidProviderTarget(ref) {
			t.Errorf("ValidProviderTarget(%q) = false", ref)
		}
	}
	for _, ref := range []string{"plugin//deepseek/v4", "plugin/acme/deepseek/v4/extra", "plugin/ac me/deepseek/v4"} {
		if ValidProviderTarget(ref) {
			t.Errorf("ValidProviderTarget(%q) = true", ref)
		}
	}
	// Two segments are an ordinary ref whatever the first one is named, so
	// "plugin/acme" addresses a model called acme and owns no package.
	if !ValidProviderTarget("plugin/acme") {
		t.Error(`ValidProviderTarget("plugin/acme") = false; the two-segment form is an ordinary ref`)
	}
}
