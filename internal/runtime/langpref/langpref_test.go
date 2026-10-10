package langpref

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

// Two thresholds and a 30-word cue list decided this before. Both are gone: the
// prose left after code tokens is weighed script against script, so naming an
// English symbol does not make a Chinese request English, and quoting a Chinese
// term does not make an English one Chinese.
func TestReasoningLanguageWeighsProseNotMentions(t *testing.T) {
	chinese := []string{
		"看下 parser.go", "修复 parser", "帮我 review", "优化 build.sh", "继续",
		"看下 run.log 里的 IndexError 是怎么来的",
		"修复 app.py 的 parse 函数，run.log 里有 Traceback",
		"这个 bug 怎么修：IndexError: list index out of range in app.py line 2",
		"把 internal/runtime/agent/agent.go 里的 SetGate 拆出来",
		"为什么 CI 一直红？把 go vet 的输出贴出来看看",
		"PR #123 修复了什么问题",
	}
	for _, in := range chinese {
		if got := InferReasoningLanguageFromText(in); got != "zh" {
			t.Errorf("%q = %s, want zh", in, got)
		}
	}
	english := []string{
		"Add a 中文 test", "grep for 问题 in the logs", "rename 代码 to code",
		"Fix the 编码 bug", "Document the 设置 flag",
		"Refactor the parser and add tests",
		"The error message says 文件不存在 — where does that string live?",
		"Translate the 用户手册 section into English",
		"why does normalizeName strip 全角 characters?",
	}
	for _, in := range english {
		if got := InferReasoningLanguageFromText(in); got != "auto" {
			t.Errorf("%q = %s, want auto: naming a Chinese term is not writing in Chinese", in, got)
		}
	}
}

func TestCodeTokensCarryNoLanguage(t *testing.T) {
	for _, token := range []string{"parser.go", "internal/runtime/agent", "run.log", "snake_case", "camelCase", "IndexError", "`inline`", "a/b/c"} {
		if !tokenIsCode(token) {
			t.Errorf("%q should not vote on the prose language", token)
		}
	}
	for _, token := range []string{"parser", "Add", "the", "问题", "CI", "SQL"} {
		if tokenIsCode(token) {
			t.Errorf("%q is prose, not code", token)
		}
	}
}

func TestResolveResponseLanguage(t *testing.T) {
	cases := []struct{ lang, source, want string }{
		{"auto", "请帮我看看这个函数为什么超时", "zh"},
		{"auto", "why does this function time out", "auto"},
		{"auto", "この関数がなぜタイムアウトするのか見て", "auto"},
		{"en", "请帮我看看", "en"},
		{"zh", "hello", "zh"},
	}
	for _, c := range cases {
		if got := ResolveResponseLanguage(c.lang, c.source); got != c.want {
			t.Errorf("ResolveResponseLanguage(%q, %q) = %q, want %q", c.lang, c.source, got, c.want)
		}
	}
}
