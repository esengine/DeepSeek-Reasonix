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
