package main

import (
	"strings"
	"testing"
)

func TestPlanSegmentsIsOneLegByDefault(t *testing.T) {
	task := task{ID: "x", Prompt: "fix it", MaxSteps: 12}
	for _, count := range []int{0, 1} {
		got := planSegments(task, count, nil)
		if len(got) != 1 || got[0].resume || got[0].prompt != "fix it" || got[0].maxSteps != 12 {
			t.Fatalf("count %d = %+v, want the unsegmented run", count, got)
		}
	}
}

// The step budget is the task's contract; splitting must not quietly hand a
// segmented arm more work than the control arm gets.
func TestPlanSegmentsPreservesTheStepBudget(t *testing.T) {
	for _, steps := range []int{12, 13, 20, 7} {
		got := planSegments(task{Prompt: "p", MaxSteps: steps}, 3, nil)
		total := 0
		for _, seg := range got {
			if seg.maxSteps < 1 {
				t.Fatalf("steps %d produced an empty leg: %+v", steps, got)
			}
			total += seg.maxSteps
		}
		if total != steps {
			t.Fatalf("steps %d split into %d, want the budget preserved", steps, total)
		}
	}
}

func TestPlanSegmentsResumesEveryLegAfterTheFirst(t *testing.T) {
	got := planSegments(task{Prompt: "fix it", MaxSteps: 9}, 3, nil)
	if len(got) != 3 {
		t.Fatalf("legs = %d, want 3", len(got))
	}
	if got[0].resume || got[0].prompt != "fix it" {
		t.Fatalf("leg 1 = %+v, want the task itself on a fresh session", got[0])
	}
	for _, seg := range got[1:] {
		if !seg.resume {
			t.Fatalf("leg %d must resume: %+v", seg.index, seg)
		}
		if strings.Contains(seg.prompt, "fix it") {
			t.Fatalf("leg %d restates the task, which hides the degradation being measured: %q", seg.index, seg.prompt)
		}
	}
}

func TestPlanSegmentsDeliversSteering(t *testing.T) {
	steers, err := parseSteers("also handle empty input@2")
	if err != nil {
		t.Fatalf("parseSteers: %v", err)
	}
	got := planSegments(task{Prompt: "fix it", MaxSteps: 9}, 3, steers)
	if got[1].prompt != "also handle empty input" {
		t.Fatalf("leg 2 prompt = %q, want the steering message", got[1].prompt)
	}
	if !got[1].resume {
		t.Fatalf("a steered leg still resumes: %+v", got[1])
	}
	if got[2].prompt != continuationPrompt {
		t.Fatalf("leg 3 = %q, want the plain continuation", got[2].prompt)
	}
}

func TestParseSteersRejectsLegOneAndMalformedSpecs(t *testing.T) {
	for _, bad := range []string{"msg@1", "msg@0", "msg", "@2", "msg@x"} {
		if _, err := parseSteers(bad); err == nil {
			t.Fatalf("%q must be rejected", bad)
		}
	}
	if got, err := parseSteers(""); err != nil || got != nil {
		t.Fatalf("empty spec = %v, %v", got, err)
	}
}
