package main

import (
	"strings"
	"testing"

	"reasonix/internal/agent"
	"reasonix/internal/provider"
)

func TestHistoryMessagesHideHostRecoverySteer(t *testing.T) {
	userPrompt := "请继续完成 PPT 任务，按昨天的素材清单生成。"
	userSteer := "改用 Pillow 10 验证"
	msgs := []provider.Message{
		{Role: provider.RoleUser, Content: userPrompt},
		{Role: provider.RoleAssistant, Content: "下一步：安装 pptxgenjs 并确认 Pillow。"},
		{Role: provider.RoleUser, Content: agent.MidTurnSteerPrefix + "\n" + agent.HostRecoveryGuidanceToolFailedPrefix + ", continue unrelated work automatically."},
		{Role: provider.RoleUser, Content: agent.MidTurnSteerPrefix + "\n" + userSteer},
		{Role: provider.RoleAssistant, Content: "pptxgenjs 已安装成功"},
	}
	got := historyMessages(msgs, func(content string) string { return content })
	var roles []string
	var contents []string
	for _, row := range got {
		roles = append(roles, row.Role)
		contents = append(contents, row.Content)
		if strings.Contains(row.Content, "A tool failed") || strings.Contains(row.Content, "Use read-only diagnosis") {
			t.Fatalf("host recovery guidance leaked into history: %+v", row)
		}
	}
	if strings.Join(roles, ",") != "user,assistant,notice,assistant" {
		t.Fatalf("history roles = %s, want user,assistant,notice,assistant", strings.Join(roles, ","))
	}
	if contents[0] != userPrompt {
		t.Fatalf("user prompt = %q, want original task prompt", contents[0])
	}
	if contents[2] != "↪ "+userSteer {
		t.Fatalf("user steer = %q, want visible mid-turn steer", contents[2])
	}
}
