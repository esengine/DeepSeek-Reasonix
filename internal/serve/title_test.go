package serve

import (
	"strings"
	"testing"
)

// TestPreviewTitleStripsPasteLabel guards the title fallback path: a
// paste-first session's first message starts with the desktop display label
// "[已粘贴文本 #N · M 行]" (or 已貼上文字 / Pasted text). previewTitle must
// strip the label so the session title reflects the pasted content, not UI
// chrome.
func TestPreviewTitleStripsPasteLabel(t *testing.T) {
	cases := []struct {
		label string
		first string
	}{
		{"简体", "[已粘贴文本 #1 · 100 行]\npackage main\n\nfunc main() { fmt.Println(\"hi\") }"},
		{"繁体", "[已貼上文字 #2 · 5 行]\n帮我看看这段代码有没有问题"},
		{"英文", "[Pasted text #3 · 42 lines]\nfunc foo() { return 1 }"},
	}
	for _, c := range cases {
		got := previewTitle(c.first)
		if strings.Contains(got, "已粘贴文本") || strings.Contains(got, "已貼上文字") || strings.Contains(got, "Pasted text") {
			t.Errorf("%s: previewTitle 未剥离粘贴标签，标题 = %q", c.label, got)
		}
		if !strings.Contains(got, "package main") && !strings.Contains(got, "帮我看看") && !strings.Contains(got, "func foo") {
			t.Errorf("%s: 标题应保留真实内容，标题 = %q", c.label, got)
		}
	}
}

// TestPreviewTitleKeepsShortFirstMessage: 无粘贴标签的普通消息行为不变。
func TestPreviewTitleKeepsShortFirstMessage(t *testing.T) {
	if got := previewTitle("帮我调试登录问题"); got != "帮我调试登录问题" {
		t.Fatalf("短消息应原样返回，got %q", got)
	}
}
