package agent

import "testing"

// TestHeuristicInputIsTask covers the delivery evidence gate's task-vs-chat
// heuristic: greetings and acknowledgements must not arm the delivery gates,
// while actionable requests and failure descriptions must.
func TestHeuristicInputIsTask(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		// Greetings / acknowledgements — chat.
		{"hello", "hello", false},
		{"hi", "hi", false},
		{"你好", "你好", false},
		{"thanks", "thanks", false},
		{"谢谢", "谢谢", false},
		{"ok", "ok", false},
		{"好的", "好的", false},
		{"收到", "收到", false},
		{"我知道了", "我知道了", false},
		{"先不用", "先不用", false},

		// Explicit tasks.
		{"fix bug", "fix the bug", true},
		{"create component", "create a component", true},
		{"修复问题", "修复这个问题", true},
		{"run tests", "run tests", true},
		{"modify config", "modify config", true},
		{"make changes", "make the requested changes", true},
		{"adjust config", "调整现有配置", true},
		{"push branch", "push the branch", true},
		{"publish release", "publish release", true},
		{"看看", "帮我看看这个错误", true},
		{"帮我看下", "帮我看下这个问题", true},
		{"处理下", "处理下这个 issue", true},
		{"排查", "排查一下启动失败", true},
		{"定位", "定位这个异常", true},

		// Conversational acknowledgements that contain task words.
		{"thanks for fixing", "thanks for fixing that!", false},
		{"check later", "I'll check later", false},
		{"test later", "I'll test it later", false},
		{"test was helpful", "that test was helpful", false},
		{"辛苦了", "辛苦了", false},
		{"thanks then update", "thanks for fixing that, now update the tests", true},
		{"chinese thanks then update", "谢谢你，请继续修改配置", true},
		{"task before thanks", "review this PR; thanks for the help", true},

		// Actionable problem descriptions without imperative verbs.
		{"auth not working", "the auth isn't working", true},
		{"help with login", "can you help with login?", true},
		{"问题严重", "这个问题很严重", true},
		{"卡住了", "页面卡住了", true},
		{"没反应", "按钮点击没反应", true},
		{"不生效", "配置不生效", true},
		{"异常退出", "程序异常退出", true},

		// File references.
		{"file reference", "what about @auth.go", true},
		{"localized file reference", "检查（@配置.yaml）", true},
		{"python file", "check main.py", true},
		{"markdown file", "why does README.md render incorrectly", true},
		{"relative script", "why does ./scripts/verify.sh fail", true},
		{"email is not file reference", "thanks, email me@example.com later", false},

		// Edge cases.
		{"empty", "", false},
		{"spaces", "   ", false},
		{"question mark", "?", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := heuristicInputIsTask(tt.input); got != tt.want {
				t.Errorf("heuristicInputIsTask(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}
