package bot

import (
	"fmt"
	"strings"

	"reasonix/internal/event"
)

func isWriteAccessApproval(a event.Approval) bool {
	return strings.EqualFold(strings.TrimSpace(a.Kind), event.ApprovalKindWriteAccess) || a.WriteAccess != nil
}

func writeAccessKeyboard(id string) *InlineKeyboard {
	return &InlineKeyboard{Rows: []InlineKeyboardRow{{
		Buttons: []InlineKeyboardButton{
			{ID: "allow_once", Label: "允许一次", Style: 1, CallbackID: "/approve " + id},
			{ID: "allow_session", Label: "本会话允许", Style: 0, CallbackID: "/approve-session " + id},
			{ID: "allow_project", Label: "加入项目", Style: 0, CallbackID: "/approve-project " + id},
			{ID: "deny", Label: "拒绝", Style: 2, CallbackID: "/deny " + id},
		},
	}}}
}

func writeAccessCard(a event.Approval, chatType ChatType, userID string) *InteractiveCard {
	return &InteractiveCard{
		Header: "需要扩展写入范围",
		Elements: []InteractiveCardElement{
			{Tag: "markdown", Content: renderWriteAccessText(a)},
			{Tag: "action", Extra: map[string]any{
				"actions": []map[string]any{
					{"tag": "button", "text": map[string]string{"tag": "plain_text", "content": "允许一次"}, "type": "primary", "value": cardActionValue("/approve "+a.ID, chatType, userID)},
					{"tag": "button", "text": map[string]string{"tag": "plain_text", "content": "本会话允许"}, "type": "default", "value": cardActionValue("/approve-session "+a.ID, chatType, userID)},
					{"tag": "button", "text": map[string]string{"tag": "plain_text", "content": "加入项目"}, "type": "default", "value": cardActionValue("/approve-project "+a.ID, chatType, userID)},
					{"tag": "button", "text": map[string]string{"tag": "plain_text", "content": "拒绝"}, "type": "danger", "value": cardActionValue("/deny "+a.ID, chatType, userID)},
				},
			}},
		},
	}
}

func renderWriteAccessText(a event.Approval) string {
	var b strings.Builder
	b.WriteString("⚠️ 需要扩展写入范围\n")
	fmt.Fprintf(&b, "工具: %s\n操作: %s\n", a.Tool, a.Subject)
	if wa := a.WriteAccess; wa != nil {
		dirs := wa.DisplayDirectories
		if len(dirs) == 0 {
			dirs = wa.Directories
		}
		if len(dirs) > 0 {
			fmt.Fprintf(&b, "目录: %s\n", strings.Join(dirs, ", "))
		}
		if wa.Justification != "" {
			fmt.Fprintf(&b, "原因: %s\n", wa.Justification)
		}
		if wa.BroadHomeAccess {
			b.WriteString("警告: 这将授权写入整个用户主目录。Reasonix 会话和运行时状态仍受保护。\n")
		}
		if wa.OrdinaryPermissionNeeded {
			b.WriteString("此选择也会授权当前匹配操作。\n")
		}
	}
	fmt.Fprintf(&b, "\nID: `%s`\n回复 1 允许一次，2 本会话允许，3 加入项目，4 拒绝。", a.ID)
	return b.String()
}
