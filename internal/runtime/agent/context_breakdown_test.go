package agent

import (
	"reasonix/internal/state/sessionstore"
	"testing"

	"reasonix/internal/contract/provider"
)

// The gauge says a session is at 70%; this says what put it there. Each class
// is measured with the same estimator the thresholds use, so the parts describe
// the same prompt the gauge does rather than a second opinion about it.
func TestContextBreakdownSeparatesWhatFillsTheWindow(t *testing.T) {
	sess := sessionstore.NewSession("你是一个助手")
	a := New(nil, nil, sess, Options{ContextWindow: 128000}, nil)
	sess.Add(provider.Message{Role: provider.RoleUser, Content: "把这个仓库跑一遍测试"})
	sess.Add(provider.Message{Role: provider.RoleAssistant, Content: "好，我先看构建脚本"})
	sess.Add(provider.Message{Role: provider.RoleTool, Content: string(make([]byte, 4000))})

	b := a.ContextBreakdown()
	if b.User == 0 || b.Reply == 0 || b.Output == 0 {
		t.Fatalf("every class that has messages must report tokens: %+v", b)
	}
	if b.Output <= b.User {
		t.Fatalf("a 4KB tool result must outweigh a one-line prompt: %+v", b)
	}
	if b.Total == 0 {
		t.Fatalf("total must match the gauge, got %+v", b)
	}
}

// An empty class costs nothing to report and must not be guessed at.
func TestContextBreakdownReportsZeroForClassesWithNoMessages(t *testing.T) {
	a := New(nil, nil, sessionstore.NewSession("你是一个助手"), Options{ContextWindow: 128000}, nil)
	b := a.ContextBreakdown()
	if b.User != 0 || b.Reply != 0 || b.Output != 0 {
		t.Fatalf("a fresh session has no turns yet: %+v", b)
	}
}

// The breakdown carries the boundary in force, read from the same rule that
// fires it rather than recomputed by whoever draws it.
func TestContextBreakdownCarriesTheBoundaryInForce(t *testing.T) {
	a := New(nil, nil, sessionstore.NewSession("你是一个助手"), Options{ContextWindow: 1_000_000}, nil)
	if got, want := a.ContextBreakdown().CompactAt, 850_000; got != want {
		t.Fatalf("CompactAt = %d, want capacity boundary %d against a 1M window", got, want)
	}

	// Under a window small enough that its capacity share is the lower of the
	// two, the same field must follow the other bound.
	small := New(nil, nil, sessionstore.NewSession("你是一个助手"), Options{ContextWindow: 128_000}, nil)
	if got, want := small.ContextBreakdown().CompactAt, small.window().compactTrigger(); got != want {
		t.Fatalf("CompactAt = %d, want the capacity share %d", got, want)
	}
	if small.ContextBreakdown().CompactAt >= small.ContextBreakdown().Window {
		t.Fatal("a capacity-bound session must fold before its window, not at it")
	}
}

// An undeclared window is what turns automatic maintenance off, and it must
// read as "no boundary" rather than as a boundary of zero tokens away.
func TestContextBreakdownReportsNoBoundaryWithoutAWindow(t *testing.T) {
	a := New(nil, nil, sessionstore.NewSession("你是一个助手"), Options{}, nil)
	if got := a.ContextBreakdown().CompactAt; got != 0 {
		t.Fatalf("CompactAt = %d, want 0 when nobody declared a window", got)
	}
}
