package usecap

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestALeadIsPickableAndWhole(t *testing.T) {
	long := "Dispatch 2-64 sub-agent tasks as a small dependency graph. Each item may select a profile, model, effort, tools, write_paths, or read_only."
	lead := capabilityLead(long)
	if lead != "Dispatch 2-64 sub-agent tasks as a small dependency graph." {
		t.Fatalf("lead = %q, want the first sentence", lead)
	}
	// No sentence break: bounded on a word, and never mid-character.
	cjk := strings.Repeat("在隔离的子代理里探索代码库", 30)
	got := capabilityLead(cjk)
	if len(got) > capabilityLeadBytes+4 {
		t.Fatalf("lead is %d bytes, over the bound", len(got))
	}
	if !utf8.ValidString(got) {
		t.Fatalf("lead cut a character in half: %q", got)
	}
	if capabilityLead("") != "" || capabilityLead("   ") != "" {
		t.Fatal("an absent description became text")
	}
	if got := capabilityLead("Short one."); got != "Short one." {
		t.Fatalf("a short description was rewritten: %q", got)
	}
}
