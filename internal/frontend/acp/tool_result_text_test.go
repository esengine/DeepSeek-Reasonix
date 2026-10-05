package acp

import (
	"strings"
	"testing"
)

// A result the host marked as a whole diff is fenced for the client so it can
// colour +/- lines; an unmarked result is passed through as before.
func TestToolResultTextFencesMarkedDiff(t *testing.T) {
	diff := "diff --git a/x b/x\n--- a/x\n+++ b/x\n@@ -1 +1 @@\n-old\n+new\n"
	got := toolResultText(diff, true)
	if !strings.HasPrefix(got, "```diff\n") || !strings.HasSuffix(got, "\n```") {
		t.Fatalf("marked diff not fenced:\n%q", got)
	}
	if !strings.Contains(got, "+new") {
		t.Fatalf("fence lost the body:\n%q", got)
	}
}

func TestToolResultTextLeavesUnmarkedUnfenced(t *testing.T) {
	if got := toolResultText("plain output\n", false); got != "plain output\n" {
		t.Fatalf("unmarked result changed: %q", got)
	}
}

// A marked diff is the client's to render, so it crosses the wire untruncated
// even past the display clip that bounds every other result.
func TestToolResultTextKeepsMarkedDiffUntruncated(t *testing.T) {
	body := "--- a/x\n+++ b/x\n@@ -1 +1 @@\n" + strings.Repeat("+line\n", maxResultChars)
	got := toolResultText(body, true)
	if strings.Contains(got, "chars truncated") {
		t.Fatalf("marked diff was clipped:\n%q", got[len(got)-80:])
	}
	if !strings.HasSuffix(got, "\n```") {
		t.Fatalf("marked diff not fenced:\n%q", got[len(got)-40:])
	}
}

// An unmarked result longer than the clip is still truncated.
func TestToolResultTextClipsUnmarked(t *testing.T) {
	got := toolResultText(strings.Repeat("x", maxResultChars+50), false)
	if !strings.Contains(got, "chars truncated") {
		t.Fatalf("unmarked result not clipped")
	}
}

// A ``` line inside the diff is context, not a fence: the wrapper has to be
// longer than any backtick run in the body so the client cannot close it early.
func TestToolResultTextOutrunsBackticksInBody(t *testing.T) {
	diff := "--- a/x\n+++ b/x\n@@ -1 +1 @@\n-```\n+````\n"
	got := toolResultText(diff, true)
	if !strings.HasPrefix(got, "`````diff\n") {
		t.Fatalf("fence shorter than the body's backtick run:\n%q", got)
	}
	if !strings.HasSuffix(got, "\n`````") {
		t.Fatalf("closing fence shorter than the body's backtick run:\n%q", got)
	}
}
