package main

import "testing"

// The diverged-recovery marker is only useful if it survives the shared status
// whitelist. An earlier revision set a status string the whitelist dropped, so
// the project tree silently lost the prompt.
func TestNormalizeTopicStatusKeepsDivergedRecovery(t *testing.T) {
	if got := normalizeTopicStatus(topicStatusDivergedRecovery); got != topicStatusDivergedRecovery {
		t.Fatalf("normalizeTopicStatus(%q) = %q, want it preserved", topicStatusDivergedRecovery, got)
	}
}

func TestNormalizeTopicStatusStillRejectsUnknown(t *testing.T) {
	if got := normalizeTopicStatus("not_a_status"); got != "" {
		t.Fatalf("normalizeTopicStatus(unknown) = %q, want empty", got)
	}
}
