package agent

import (
	"testing"
)

// TestReasoningLanguageDirectiveIsSynthetic: 宿主按用户配置在每轮注入的
// "<reasoning-language> …" 指令（role=user）不是用户手打内容，不得成为
// 会话预览/标题/turn 计数的一部分（用户真实会话中它曾是 first 消息，
// 导致标题变成 "<reasoning-language> 必须使用简体中文…"）。
func TestReasoningLanguageDirectiveIsSynthetic(t *testing.T) {
	injected := "<reasoning-language>\n必须使用简体中文书写全部可见思考/推理文本：从第一个字开始就用中文"
	if IsUserAuthoredTurn(injected) {
		t.Fatalf("<reasoning-language> 注入指令不应被算作 user-authored")
	}
}

// TestStripPasteDisplayLabel: 剥离桌面端粘贴标签（简体/繁体/英文三种形态），
// 无标签文本保持不变。
func TestStripPasteDisplayLabel(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"简体", "[已粘贴文本 #1 · 100 行]\npackage main\n\nfunc main() {}",
			"package main\n\nfunc main() {}"},
		{"繁体", "[已貼上文字 #2 · 5 行]\n帮我看看这段代码", "帮我看看这段代码"},
		{"英文", "[Pasted text #3 · 42 lines]\nfunc foo() {}", "func foo() {}"},
		{"标签在行中不误删", "看这个 [已粘贴文本 #1 · 2 行] 的处理", "看这个 [已粘贴文本 #1 · 2 行] 的处理"},
		{"无标签", "帮我调试登录问题", "帮我调试登录问题"},
	}
	for _, c := range cases {
		if got := StripPasteDisplayLabel(c.in); got != c.want {
			t.Errorf("%s: StripPasteDisplayLabel(%q) = %q, want %q", c.name, c.in, got, c.want)
		}
	}
}
