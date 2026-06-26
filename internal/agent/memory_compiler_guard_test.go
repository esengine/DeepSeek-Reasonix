package agent

import (
	"testing"
	"time"
)

func TestShouldInjectCompilerRejectsEmptyInput(t *testing.T) {
	a := &Agent{
		compilerInjectionMax:   5,
		compilerInjectCooldown: 30 * time.Second,
	}
	if a.shouldInjectCompiler("") {
		t.Fatal("shouldInjectCompiler(\"\") = true, want false")
	}
	if a.shouldInjectCompiler("   ") {
		t.Fatal("shouldInjectCompiler(\"   \") = true, want false")
	}
}

func TestShouldInjectCompilerRejectsSystemGeneratedInput(t *testing.T) {
	a := &Agent{
		compilerInjectionMax:   5,
		compilerInjectCooldown: 30 * time.Second,
	}
	for _, input := range []string{
		"<memory-compiler-execution>...",
		"<steer>some steer</steer>",
		"<background-jobs>done</background-jobs>",
	} {
		if a.shouldInjectCompiler(input) {
			t.Fatalf("shouldInjectCompiler(%q) = true, want false for system-generated input", input)
		}
	}
}

func TestShouldInjectCompilerAcceptsGenuineInput(t *testing.T) {
	a := &Agent{
		compilerInjectionMax:   5,
		compilerInjectCooldown: 30 * time.Second,
	}
	if !a.shouldInjectCompiler("fix the login bug") {
		t.Fatal("shouldInjectCompiler(\"fix the login bug\") = false, want true")
	}
	if !a.shouldInjectCompiler("今日分时交易数据分析") {
		t.Fatal("shouldInjectCompiler(Chinese text) = false, want true")
	}
}

func TestShouldInjectCompilerEnforcesCooldown(t *testing.T) {
	a := &Agent{
		compilerInjectionMax:   5,
		compilerInjectCooldown: 30 * time.Second,
	}
	if !a.shouldInjectCompiler("first message") {
		t.Fatal("first shouldInjectCompiler = false, want true")
	}
	a.markCompilerInjected()
	if a.shouldInjectCompiler("second message") {
		t.Fatal("shouldInjectCompiler during cooldown = true, want false")
	}
}

func TestShouldInjectCompilerAllowsAfterCooldown(t *testing.T) {
	a := &Agent{
		compilerInjectionMax:   5,
		compilerInjectCooldown: 30 * time.Second,
	}
	a.shouldInjectCompiler("first message")
	a.markCompilerInjected()
	// simulate cooldown elapsed
	a.lastCompilerInjectedAt = time.Now().Add(-31 * time.Second)
	if !a.shouldInjectCompiler("second message") {
		t.Fatal("shouldInjectCompiler after cooldown = false, want true")
	}
}

func TestShouldInjectCompilerEnforcesSessionCap(t *testing.T) {
	a := &Agent{
		compilerInjectionMax:   3,
		compilerInjectCooldown: 0, // no cooldown for this test
	}
	for i := 0; i < 3; i++ {
		if !a.shouldInjectCompiler("message") {
			t.Fatalf("shouldInjectCompiler #%d = false, want true (cap not reached yet)", i+1)
		}
		a.markCompilerInjected()
	}
	if a.shouldInjectCompiler("one too many") {
		t.Fatal("shouldInjectCompiler after cap = true, want false")
	}
}

func TestShouldInjectCompilerZeroMaxDisablesAll(t *testing.T) {
	a := &Agent{
		compilerInjectionMax:   0,
		compilerInjectCooldown: 30 * time.Second,
	}
	if a.shouldInjectCompiler("any message") {
		t.Fatal("shouldInjectCompiler with max=0 = true, want false")
	}
}

func TestMarkCompilerInjectedTracksState(t *testing.T) {
	a := &Agent{
		compilerInjectionMax:   5,
		compilerInjectCooldown: 30 * time.Second,
	}
	if a.compilerInjectionCount != 0 {
		t.Fatalf("initial count = %d, want 0", a.compilerInjectionCount)
	}
	a.markCompilerInjected()
	if a.compilerInjectionCount != 1 {
		t.Fatalf("count after mark = %d, want 1", a.compilerInjectionCount)
	}
	if a.lastCompilerInjectedAt.IsZero() {
		t.Fatal("lastCompilerInjectedAt not set after mark")
	}
}
