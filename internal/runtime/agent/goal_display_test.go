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

// Planner conclusion markers are coordinator protocol; the display filter
// removes the standalone lines while parsing keeps reading the raw text
// (#6789: a relayed answer-only plan must not show "[no_changes]").
func TestDisplayFilterStripsPlannerConclusionMarkers(t *testing.T) {
	in := "The two agents differ mainly in orchestration.\n\n[no_changes]"
	want := "The two agents differ mainly in orchestration."
	if got := DisplayAssistantText(in); got != want {
		t.Errorf("DisplayAssistantText = %q, want %q", got, want)
	}
	in = "Plan ready for review.\n[planner_requires_approval]"
	want = "Plan ready for review."
	if got := StripGoalMarkers(in); got != want {
		t.Errorf("StripGoalMarkers = %q, want %q", got, want)
	}
	// Inline mentions survive — only standalone marker lines are protocol.
	in = "The [no_changes] marker is emitted when done."
	if got := DisplayAssistantText(in); got != in {
		t.Errorf("inline mention altered: %q", got)
	}
}
