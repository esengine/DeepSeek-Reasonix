package agent

import (
	"strings"
	"testing"
)

func TestWithResponseLanguageOnlySkipsLeadingInjectedBlock(t *testing.T) {
	userMention := "explain why <response-language> appears in this file"
	got := WithResponseLanguage(userMention, "en")
	if !strings.HasPrefix(got, "<response-language>") || !strings.Contains(got, "use English") || !strings.HasSuffix(got, userMention) {
		t.Fatalf("WithResponseLanguage should prefix user-authored tag mentions, got %q", got)
	}

	alreadyPrefixed := ResponseLanguageBlock("en") + "\n\n" + userMention
	if got := WithResponseLanguage(alreadyPrefixed, "en"); got != alreadyPrefixed {
		t.Fatalf("WithResponseLanguage duplicated a leading injected block:\n got %q\nwant %q", got, alreadyPrefixed)
	}

	withLeadingMemory := "<memory-update>\nRemember this.\n</memory-update>\n\n" + alreadyPrefixed
	if got := WithResponseLanguage(withLeadingMemory, "en"); got != withLeadingMemory {
		t.Fatalf("WithResponseLanguage duplicated a response block after leading transient context:\n got %q\nwant %q", got, withLeadingMemory)
	}
}

func TestWithReasoningLanguageOnlySkipsLeadingInjectedBlock(t *testing.T) {
	userMention := "explain why <reasoning-language> appears in this file"
	got := WithReasoningLanguage(userMention, "zh")
	if !strings.HasPrefix(got, "<reasoning-language>") || !strings.Contains(got, "简体中文") || !strings.HasSuffix(got, userMention) {
		t.Fatalf("WithReasoningLanguage should prefix user-authored tag mentions, got %q", got)
	}

	alreadyPrefixed := ReasoningLanguageBlock("zh") + "\n\n" + userMention
	if got := WithReasoningLanguage(alreadyPrefixed, "zh"); got != alreadyPrefixed {
		t.Fatalf("WithReasoningLanguage duplicated a leading injected block:\n got %q\nwant %q", got, alreadyPrefixed)
	}

	withLeadingMemory := "<memory-update>\nRemember this.\n</memory-update>\n\n" + alreadyPrefixed
	if got := WithReasoningLanguage(withLeadingMemory, "zh"); got != withLeadingMemory {
		t.Fatalf("WithReasoningLanguage duplicated a reasoning block after leading transient context:\n got %q\nwant %q", got, withLeadingMemory)
	}
}

func TestReasoningLanguageBlockZhStaysImperative(t *testing.T) {
	// The imperative form measurably outperforms soft "偏好" phrasing on
	// Chinese prompts that embed English logs/code; keep it from regressing
	// back into a suggestion.
	block := ReasoningLanguageBlock("zh")
	for _, want := range []string{"必须使用简体中文", "整轮", "不覆盖用户对最终回答语言的明确要求"} {
		if !strings.Contains(block, want) {
			t.Fatalf("zh reasoning block lost required anchor %q:\n%s", want, block)
		}
	}
}

func TestWithReasoningLanguageAutoInfersFromSource(t *testing.T) {
	chinese := WithReasoningLanguage("解释 AuthHandler 的 panic", "auto")
	if !strings.HasPrefix(chinese, "<reasoning-language>") || !strings.Contains(chinese, "简体中文") {
		t.Fatalf("auto reasoning language should infer Chinese, got %q", chinese)
	}

	english := WithReasoningLanguage("explain this module", "auto")
	if english != "explain this module" {
		t.Fatalf("auto reasoning language should keep English prompts unwrapped, got %q", english)
	}

	short := WithReasoningLanguage("hi", "auto")
	if short != "hi" {
		t.Fatalf("short ambiguous auto prompt should not be wrapped, got %q", short)
	}
}

func TestWithReasoningLanguageAutoUsesRawSourceOverReferencedContext(t *testing.T) {
	expanded := "Referenced context:\n\n<file path=\"auth.go\">\npackage main\nfunc AuthHandler() error { return errors.New(\"not authorized\") }\n</file>\n\n解释 @auth.go 的报错"

	got := WithReasoningLanguageForSource(expanded, "auto", "解释 @auth.go 的报错")
	if !strings.HasPrefix(got, "<reasoning-language>") || !strings.Contains(got, "简体中文") {
		t.Fatalf("auto reasoning language should use raw source over referenced context, got %q", got)
	}
	if strings.Contains(got, "use English") {
		t.Fatalf("referenced English code should not make auto prefer English:\n%s", got)
	}
}

func TestReasoningLanguageOffDisablesInjection(t *testing.T) {
	// NormalizeReasoningLanguage
	for _, input := range []string{"off", "none", "disable"} {
		if got := NormalizeReasoningLanguage(input); got != "off" {
			t.Errorf("NormalizeReasoningLanguage(%q) = %q, want off", input, got)
		}
	}

	// ResolveReasoningLanguage returns empty for off regardless of source
	if got := ResolveReasoningLanguage("off", "帮我看看这个代码"); got != "" {
		t.Fatalf("ResolveReasoningLanguage(off, Chinese) = %q, want empty", got)
	}
	if got := ResolveReasoningLanguage("off", "explain this"); got != "" {
		t.Fatalf("ResolveReasoningLanguage(off, English) = %q, want empty", got)
	}
	if got := ResolveReasoningLanguage("off", "hi"); got != "" {
		t.Fatalf("ResolveReasoningLanguage(off, short) = %q, want empty", got)
	}

	// ReasoningLanguageBlock returns empty for off
	if got := ReasoningLanguageBlock("off"); got != "" {
		t.Fatalf("ReasoningLanguageBlock(off) = %q, want empty", got)
	}

	// WithReasoningLanguage passes content through unchanged
	content := "帮我解释这个 panic"
	got := WithReasoningLanguage(content, "off")
	if got != content {
		t.Fatalf("WithReasoningLanguage(content, off) should not inject, got %q", got)
	}

	// WithReasoningLanguageForSource with off and any source
	got = WithReasoningLanguageForSource(content, "off", "中文source")
	if got != content {
		t.Fatalf("WithReasoningLanguageForSource(content, off, Chinese) should not inject, got %q", got)
	}

	// Leading transient blocks are skipped, still no injection
	withMemory := "<memory-update>\nRemember this.\n</memory-update>\n\n" + content
	got = WithReasoningLanguage(withMemory, "off")
	if got != withMemory {
		t.Fatalf("WithReasoningLanguage(leading memory, off) should not inject, got %q", got)
	}

	// off beats auto: auto with Chinese would inject, off should not
	if WithReasoningLanguage(content, "auto") == content {
		t.Fatal("sanity check: auto with Chinese content should inject, but it did not")
	}
	if WithReasoningLanguage(content, "off") != content {
		t.Fatal("off with same Chinese content should NOT inject, but it did")
	}
}
