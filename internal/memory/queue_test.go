package memory

import (
	"strings"
	"testing"
)

func TestTrimMemoryNoteBodyKeepsShortBodiesWhole(t *testing.T) {
	body := "Always answer in Chinese unless the user explicitly asks for English."
	if got := TrimMemoryNoteBody(body); got != body {
		t.Fatalf("TrimMemoryNoteBody(%q) = %q, want unchanged body", body, got)
	}
}

func TestTrimMemoryNoteBodyTrimsLongBodies(t *testing.T) {
	body := strings.Repeat("x", maxNoteBodyRunes+100)
	got := TrimMemoryNoteBody(body)
	if len([]rune(got)) != maxNoteBodyRunes+1 { // 400 runes + "…"
		t.Fatalf("TrimMemoryNoteBody length = %d runes, want %d", len([]rune(got)), maxNoteBodyRunes+1)
	}
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("TrimMemoryNoteBody result %q should end with ellipsis", got)
	}
	if strings.Contains(got, "x") == false || strings.Count(got, "x") != maxNoteBodyRunes {
		t.Fatalf("TrimMemoryNoteBody should keep exactly the first %d runes", maxNoteBodyRunes)
	}
}

func TestTrimMemoryNoteBodyBoundaryExactLimit(t *testing.T) {
	body := strings.Repeat("y", maxNoteBodyRunes)
	if got := TrimMemoryNoteBody(body); got != body {
		t.Fatalf("body of exactly maxNoteBodyRunes runes should be unchanged, got %d runes", len([]rune(got)))
	}
}

func TestTrimMemoryNoteBodyRuneSafeForCJK(t *testing.T) {
	// 中文字符是多字节的；截断必须按 rune 切，不能切出半个字符。
	body := strings.Repeat("记忆", maxNoteBodyRunes/2+50) // 每个"记忆"2 runes
	got := TrimMemoryNoteBody(body)
	runes := []rune(got)
	if len(runes) != maxNoteBodyRunes+1 {
		t.Fatalf("CJK body trimmed to %d runes, want %d", len(runes), maxNoteBodyRunes+1)
	}
	if string(runes[len(runes)-1]) != "…" {
		t.Fatalf("CJK trimmed body should end with ellipsis, got %q", runes[len(runes)-1])
	}
}

func TestTrimMemoryNoteBodyStripsWhitespace(t *testing.T) {
	if got := TrimMemoryNoteBody("  \n short body \t "); got != "short body" {
		t.Fatalf("TrimMemoryNoteBody should trim surrounding whitespace, got %q", got)
	}
}
