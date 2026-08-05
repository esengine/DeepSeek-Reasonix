package agent

import "testing"

func TestReasoningLanguageDirectiveIsNotUserAuthored(t *testing.T) {
	for _, injected := range []string{
		"<reasoning-language>\nUse Simplified Chinese",
		"<reasoning-language>\nUse Simplified Chinese\n</reasoning-language>",
	} {
		if IsUserAuthoredTurn(injected) {
			t.Fatalf("reasoning-language directive %q must not count as user-authored", injected)
		}
	}
}

func TestStripPasteDisplayLabel(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{name: "simplified Chinese", in: "[已粘贴文本 #1 · 100 行]\npackage main", want: "package main"},
		{name: "traditional Chinese", in: "[已貼上文字 #2 · 5 行]\r\n帮我看看这段代码", want: "帮我看看这段代码"},
		{name: "English", in: "[Pasted text #3 · 42 lines]\nfunc foo() {}", want: "func foo() {}"},
		{name: "inline mention", in: "Explain [Pasted text #1 · 2 lines] handling", want: "Explain [Pasted text #1 · 2 lines] handling"},
		{name: "later standalone line", in: "Keep this\n[Pasted text #1 · 2 lines]\nverbatim", want: "Keep this\n[Pasted text #1 · 2 lines]\nverbatim"},
		{name: "no label", in: "debug the login issue", want: "debug the login issue"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := StripPasteDisplayLabel(tt.in); got != tt.want {
				t.Fatalf("StripPasteDisplayLabel(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
