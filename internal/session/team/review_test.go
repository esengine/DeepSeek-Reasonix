package team

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"reasonix/internal/state/memory"
)

func reviewStore(t *testing.T, facts ...memory.Memory) memory.Store {
	t.Helper()
	s := memory.Store{Dir: t.TempDir()}
	for _, m := range facts {
		if _, err := s.Save(m); err != nil {
			t.Fatal(err)
		}
	}
	return s
}

type askRecorder struct {
	calls  int
	prompt string
	reply  string
}

func (a *askRecorder) ask(_ context.Context, _ string, prompt string) (string, error) {
	a.calls++
	a.prompt = prompt
	return a.reply, nil
}

func TestAReviewWithNothingThePersonSaidAsksNoModel(t *testing.T) {
	s := reviewStore(t)
	a := &askRecorder{reply: `{"changes":[{"op":"add","description":"x","body":"x"}]}`}
	got, err := ReviewMemory(context.Background(), s, []ReviewLine{{From: "Ada", Text: "done"}}, a.ask)
	if err != nil || a.calls != 0 || got != (ReviewReport{}) {
		t.Fatalf("review = %+v, %v after %d calls", got, err, a.calls)
	}
}

func TestAReviewAddsEditsAndRemovesWhatTheModelSays(t *testing.T) {
	s := reviewStore(t,
		memory.Memory{Name: "tabs", Description: "prefers tabs", Type: memory.TypeUser, Body: "tabs"},
		memory.Memory{Name: "old-branch", Description: "release branch is r1", Type: memory.TypeProject, Body: "r1"},
	)
	a := &askRecorder{reply: "Sure.\n```json\n" + `{"changes":[
		{"op":"add","description":"launch is on Friday","type":"project","body":"The launch is on Friday."},
		{"op":"edit","entry":2,"description":"prefers tabs, width 4","body":"Tabs, shown 4 wide."},
		{"op":"remove","entry":1}
	]}` + "\n```"}
	got, err := ReviewMemory(context.Background(), s, []ReviewLine{{From: "user", Text: "the launch moved to Friday; tabs at 4 please", User: true}}, a.ask)
	if err != nil || got != (ReviewReport{Added: 1, Edited: 1, Removed: 1}) {
		t.Fatalf("review = %+v, %v", got, err)
	}
	if !strings.Contains(a.prompt, "1. old-branch: release branch is r1") || !strings.Contains(a.prompt, "2. tabs: prefers tabs") {
		t.Fatalf("the prompt did not number the memory it holds:\n%s", a.prompt)
	}
	facts, _ := s.List()
	text := fmt.Sprint(facts)
	if len(facts) != 2 || !strings.Contains(text, "launch is on Friday") || !strings.Contains(text, "prefers tabs, width 4") || strings.Contains(text, "release branch is r1") {
		t.Fatalf("memory after the review: %+v", facts)
	}
}

func TestAReviewMakesAtMostEightChanges(t *testing.T) {
	s := reviewStore(t)
	var b strings.Builder
	b.WriteString(`{"changes":[`)
	for i := range 12 {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, `{"op":"add","description":"fact number %d","body":"fact %d"}`, i, i)
	}
	b.WriteString(`]}`)
	a := &askRecorder{reply: b.String()}
	got, err := ReviewMemory(context.Background(), s, []ReviewLine{{From: "user", Text: "lots", User: true}}, a.ask)
	if err != nil || got.Added != reviewMaxChanges {
		t.Fatalf("review = %+v, %v", got, err)
	}
}

func TestAnUnreadableReplyChangesNothingAndSaysSo(t *testing.T) {
	s := reviewStore(t, memory.Memory{Name: "tabs", Description: "prefers tabs", Type: memory.TypeUser, Body: "tabs"})
	a := &askRecorder{reply: "I think the user likes tabs."}
	if _, err := ReviewMemory(context.Background(), s, []ReviewLine{{From: "user", Text: "tabs", User: true}}, a.ask); !errors.Is(err, ErrReviewReply) {
		t.Fatalf("an unreadable reply = %v, want ErrReviewReply", err)
	}
	if facts, _ := s.List(); len(facts) != 1 {
		t.Fatalf("an unreadable reply changed memory: %+v", facts)
	}
}

func TestALongLineKeepsItsHeadAndTail(t *testing.T) {
	s := reviewStore(t)
	a := &askRecorder{reply: `{"changes":[]}`}
	long := "HEAD " + strings.Repeat("word ", 1000) + " TAIL"
	if _, err := ReviewMemory(context.Background(), s, []ReviewLine{{From: "user", Text: long, User: true}}, a.ask); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(a.prompt, "HEAD") || !strings.Contains(a.prompt, "TAIL") || strings.Count(a.prompt, "word") > reviewUserChars/5 {
		t.Fatalf("a long line reached the prompt as %d chars", len(a.prompt))
	}
}
