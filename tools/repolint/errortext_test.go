package main

import "testing"

// The direct form is already gone from the tree; what it turns into is storing
// the text first. A rule that only caught the direct form would catch nothing.
func TestErrorTextFollowsTheTextIntoALocal(t *testing.T) {
	src := `package p

import "strings"

func f(err error) bool {
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "stalled")
}
`
	got := checkErrorText(parseBytes("x.go", []byte(src)))
	if len(got) != 1 {
		t.Fatalf("findings = %d, want 1: %v", len(got), got)
	}
	if got[0].Rule != ruleErrorText || got[0].Weight != 1 {
		t.Fatalf("a pass/fail rule must weigh one: %+v", got[0])
	}
}

func TestErrorTextFlagsTheDirectFormToo(t *testing.T) {
	src := `package p

import "strings"

func f(err error) bool { return strings.HasPrefix(err.Error(), "x") }
`
	if got := checkErrorText(parseBytes("x.go", []byte(src))); len(got) != 1 {
		t.Fatalf("findings = %d, want 1: %v", len(got), got)
	}
}

// Reading structure is the whole point, so the rule must leave it alone —
// including a message being built rather than matched.
func TestErrorTextIgnoresIdentityAndPlainStrings(t *testing.T) {
	src := `package p

import (
	"errors"
	"fmt"
	"strings"
)

var ErrX = errors.New("x")

func f(err error, name string) string {
	if errors.Is(err, ErrX) {
		return "known"
	}
	if strings.Contains(name, "y") {
		return "plain string"
	}
	return fmt.Sprintf("%v", err.Error())
}
`
	if got := checkErrorText(parseBytes("x.go", []byte(src))); len(got) != 0 {
		t.Fatalf("structure or plain strings were flagged: %v", got)
	}
}
