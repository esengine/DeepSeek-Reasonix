package agent

import "testing"

// TestDeliveryClassificationMatrix protects the boundary between advisory
// troubleshooting, observable read-only work, and mutation delivery.
func TestDeliveryClassificationMatrix(t *testing.T) {
	tests := []struct {
		name         string
		input        string
		wantEvidence bool
		wantMutation bool
	}{
		{"make sense", "why does this make sense?", false, false},
		{"node prose", "why does the node selection matter?", false, false},
		{"swift prose", "why does Swift concurrency work this way?", false, false},
		{"go prose", "why does this go wrong so often?", false, false},
		{"remote url", "why can't I open https://github.com?", false, false},
		{"remote analysis", "can you analyze why Outlook won't sync?", false, false},
		{"how to install", "how do I install the plugin?", false, false},
		{"chinese how to install", "如何安装插件", false, false},
		{"chinese how to modify", "怎么修改 WPS 设置", false, false},
		{"python prose", "why is Python popular?", false, false},
		{"docker prose", "why is Docker popular?", false, false},
		{"pytest prose", "why is pytest popular?", false, false},
		{"backtick python prose", "why is `Python` popular?", false, false},
		{"backtick code identifier", "what does `context.Context` mean?", false, false},
		{"double dash prose", "why is this -- strangely -- happening?", false, false},
		{"advisory upgrade", "为什么升级插件后失败", false, false},
		{"advisory adjustment", "为什么调整设置后没有效果", false, false},
		{"advisory change application", "为什么申请修改失败", false, false},
		{"negated change", "please don't make the requested changes", false, false},
		{"negated chinese", "请勿修改代码", false, false},
		{"review only", "review only and do not fix anything", true, false},
		{"markdown anchor", "why does README.md render incorrectly?", true, false},
		{"git command", "why does git diff --check fail?", true, false},
		{"custom command", "why does mytool --strict fail?", true, false},
		{"repository anchor", "can you analyze this repository and explain why it fails?", true, false},
		{"observable audit", "audit the current configuration", true, false},
		{"make changes", "make the necessary changes", true, true},
		{"thanks then update", "thanks for fixing that, now update the tests", true, true},
		{"chinese thanks then update", "谢谢你，请继续修改配置", true, true},
		{"thanks then review", "thanks for the help; now review this pull request", true, false},
		{"modify", "please modify the config", true, true},
		{"push", "push the branch", true, true},
		{"commit", "commit the fix", true, true},
		{"move", "move the file", true, true},
		{"bump", "bump the dependency", true, true},
		{"advice then fix", "why does it fail and fix it", true, true},
		{"polite fix before advice", "could you please fix why it fails", true, true},
		{"chinese polite fix before advice", "请你帮我修复为什么会失败", true, true},
		{"chinese advice then fix", "为什么失败然后修复它", true, true},
		{"negated install then patch", "I cannot install dependencies but patch the parser", true, true},
		{"shared negation", "I cannot install and update dependencies", false, false},
		{"reset negation", "I cannot install dependencies and please update config", true, true},
		{"chinese reset", "无法安装依赖请在现有配置中修改", true, true},
		{"negated request", "不想请团队修改代码", false, false},
		{"negated application", "禁止申请修改配置", false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := deliveryTaskNeedsEvidence(tt.input); got != tt.wantEvidence {
				t.Errorf("deliveryTaskNeedsEvidence(%q) = %v, want %v", tt.input, got, tt.wantEvidence)
			}
			if got := deliveryTaskNeedsMutation(tt.input); got != tt.wantMutation {
				t.Errorf("deliveryTaskNeedsMutation(%q) = %v, want %v", tt.input, got, tt.wantMutation)
			}
		})
	}
}
