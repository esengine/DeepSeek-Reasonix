package store

import "testing"

// The rule had been spelled inline in three places and was missing from the
// fourth, which is how 154 subagent transcripts reached a user's session list.
func TestIsSubagentTranscriptName(t *testing.T) {
	for _, name := range []string{
		"subagent-sub-k-202605171512.jsonl",
		"/Users/x/.reasonix/sessions/subagent-sub-1-202605171456.jsonl",
		"subagent-sub-a-202605290230.events.jsonl",
	} {
		if !IsSubagentTranscriptName(name) {
			t.Errorf("IsSubagentTranscriptName(%q) = false, want true", name)
		}
	}
	// A real session whose name merely contains the word must stay visible.
	for _, name := range []string{
		"20260813-235122.107942000-deepseek-v4-flash.jsonl",
		"my-subagent-notes.jsonl",
		"",
	} {
		if IsSubagentTranscriptName(name) {
			t.Errorf("IsSubagentTranscriptName(%q) = true, want false", name)
		}
	}
}
