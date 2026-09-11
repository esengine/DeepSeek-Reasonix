package agent

import "testing"

func TestStripAutoResearchEvidenceBlocks(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"no block", "plain answer", "plain answer"},
		{
			"single block",
			"Findings so far.\n<autoresearch-evidence>{\"id\":\"e1\"}</autoresearch-evidence>\nNext steps.",
			"Findings so far.\n\nNext steps.",
		},
		{
			"multiple blocks",
			"<autoresearch-evidence>{\"id\":\"e1\"}</autoresearch-evidence>middle<autoresearch-evidence>{\"id\":\"e2\"}</autoresearch-evidence>",
			"middle",
		},
		{
			"unterminated block drops the tail",
			"answer\n<autoresearch-evidence>{\"id\":\"e1\"}",
			"answer",
		},
	}
	for _, tc := range cases {
		if got := StripAutoResearchEvidenceBlocks(tc.in); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

// TestDisplayAssistantTextStripsAllProtocolArtifacts guards #6665: the single
// display filter must remove both goal markers and evidence blocks before
// answer text reaches any sink.
func TestDisplayAssistantTextStripsAllProtocolArtifacts(t *testing.T) {
	in := "Done with the survey.\n[goal:continue]\n<autoresearch-evidence>{\"id\":\"e1\",\"summary\":\"x\"}</autoresearch-evidence>"
	want := "Done with the survey."
	if got := DisplayAssistantText(in); got != want {
		t.Errorf("DisplayAssistantText = %q, want %q", got, want)
	}
}
