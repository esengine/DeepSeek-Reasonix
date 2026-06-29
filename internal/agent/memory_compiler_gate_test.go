package agent

import (
	"testing"
	"time"
)

func TestMemoryCompilerInjectionGateLimitsCooldownAndResets(t *testing.T) {
	a := New(nil, nil, NewSession(""), Options{}, nil)
	base := time.Date(2026, 6, 28, 8, 0, 0, 0, time.UTC)

	if !a.tryMarkMemoryCompilerInjected(base) {
		t.Fatal("first injection should be allowed")
	}
	if a.tryMarkMemoryCompilerInjected(base.Add(memoryCompilerInjectionCooldown / 2)) {
		t.Fatal("injection inside cooldown should be rejected")
	}
	for i := 1; i < memoryCompilerInjectionMax; i++ {
		if !a.tryMarkMemoryCompilerInjected(base.Add(time.Duration(i+1) * memoryCompilerInjectionCooldown)) {
			t.Fatalf("injection %d should be allowed before max", i+1)
		}
	}
	if a.tryMarkMemoryCompilerInjected(base.Add(time.Duration(memoryCompilerInjectionMax+1) * memoryCompilerInjectionCooldown)) {
		t.Fatal("injection after session max should be rejected")
	}

	a.SetSession(NewSession(""))
	if !a.tryMarkMemoryCompilerInjected(base.Add(time.Duration(memoryCompilerInjectionMax+2) * memoryCompilerInjectionCooldown)) {
		t.Fatal("new session should reset injection budget")
	}
}

func TestShouldStartMemoryCompilerRejectsHostControlText(t *testing.T) {
	cases := []struct {
		input string
		want  bool
	}{
		{input: "", want: false},
		{input: "   \n\t", want: false},
		{input: "<memory-compiler-execution>{}</memory-compiler-execution>", want: false},
		{input: " <response-language>zh</response-language>", want: false},
		{input: "fix the bug", want: true},
		{input: "Referenced context:\n\n<file path=\"x\">x</file>\n\nfix @x", want: true},
	}
	for _, tc := range cases {
		if got := shouldStartMemoryCompiler(tc.input); got != tc.want {
			t.Fatalf("shouldStartMemoryCompiler(%q) = %v, want %v", tc.input, got, tc.want)
		}
	}
}

func TestShouldInjectMemoryCompilerContractForInputRequiresActionableTask(t *testing.T) {
	cases := []struct {
		input string
		want  bool
	}{
		{input: "hello", want: false},
		{input: "hi", want: false},
		{input: "你好", want: false},
		{input: "nihao", want: false},
		{input: "thanks", want: false},
		{input: "ok", want: false},
		{input: "fix the bug", want: true},
		{input: "create file nihao", want: true},
		{input: "review this diff", want: true},
		{input: "run tests", want: true},
		{input: "帮我修复这个 bug", want: true},
		{input: "继续处理这个 issue", want: true},
		{input: "fix @auth.go", want: true},
	}
	for _, tc := range cases {
		if got := shouldInjectMemoryCompilerContractForInput(tc.input); got != tc.want {
			t.Fatalf("shouldInjectMemoryCompilerContractForInput(%q) = %v, want %v", tc.input, got, tc.want)
		}
	}
}
