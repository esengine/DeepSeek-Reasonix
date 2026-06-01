package frontmatter

import "testing"

// TestSplitParsesYAMLList probes a YAML list value (the form skills authored for
// other agent tools use for allowed-tools). The parser must surface the items so
// a subagent skill keeps its tool scoping; dropping them silently widens the
// skill to the full parent registry.
func TestSplitParsesYAMLList(t *testing.T) {
	in := "---\n" +
		"name: review\n" +
		"allowed-tools:\n" +
		"  - read_file\n" +
		"  - grep\n" +
		"---\n" +
		"body here\n"
	fm, body := Split(in)
	if fm["name"] != "review" {
		t.Fatalf("name = %q", fm["name"])
	}
	got := fm["allowed-tools"]
	if got == "" {
		t.Fatalf("allowed-tools list was dropped entirely (got empty)")
	}
	for _, want := range []string{"read_file", "grep"} {
		if !contains(got, want) {
			t.Errorf("allowed-tools %q missing %q", got, want)
		}
	}
	if body != "body here\n" {
		t.Errorf("body = %q", body)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
