package bot

import (
	"context"
	"fmt"
	"strings"

	"reasonix/internal/event"
)

// DesktopSessionInfo 是一个桌面 live 会话(tab)的快照，用于 /desktop status。
type DesktopSessionInfo struct {
	TabID         string
	Label         string
	Workspace     string
	Topic         string
	Ready         bool
	Running       bool
	PendingPrompt bool
	// Detached 标记后台运行的会话（已从可见 tab 分离，controller 仍存活）。
	Detached bool
	// Pending 列出该会话当前待处理的审批/问答，便于用户在推送丢失时仍能
	// 用 /desktop approve|answer <id> 处理。
	Pending []DesktopPendingInfo
}

// DesktopPendingInfo 是一条待处理的审批或问答的摘要。
type DesktopPendingInfo struct {
	ID   string
	Kind string // "approval" | "ask"
	Tool string
}

// DesktopWatchRoute 标识一个订阅了桌面事件的 bot 聊天。
type DesktopWatchRoute struct {
	ConnectionID string
	Domain       string
	Platform     Platform
	ChatType     ChatType
	ChatID       string
}

// Key 返回订阅表的稳定键。
func (r DesktopWatchRoute) Key() string {
	return fmt.Sprintf("%s|%s|%s|%s", r.Platform, r.ConnectionID, r.Domain, r.ChatID)
}

// DesktopBridge 由桌面端进程实现，让 bot 聊天获得对整个桌面端的上帝视角：
// 全局会话清单、事件订阅、以及对任意桌面 live 会话的远程审批/问答。
//
// 语义约定：
//   - 审批应答与桌面 UI 是"先到者赢"（controller 侧幂等，重复应答被静默忽略），
//     Approve/Answer 的返回文案应体现"以先到者为准"。
//   - 接管是"显式"的：/desktop takeover 绑定聊天与会话，桌面端会在会话
//     transcript 里看到提示；桌面用户在该会话本地发送任意消息即自动收回
//     控制并通知远端。接管期间桌面输入不被锁定（controller 侧先到者赢）。
type DesktopBridge interface {
	// Sessions 枚举当前所有桌面 live 会话（含后台 detached）。
	Sessions() []DesktopSessionInfo
	// SetWatch 订阅/退订当前聊天的桌面事件推送。
	SetWatch(route DesktopWatchRoute, enable bool) error
	// Watching 返回该聊天当前是否在订阅。
	Watching(route DesktopWatchRoute) bool
	// Approve 应答任意桌面会话的待审批项，返回用户可读的结果文案。
	Approve(approvalID string, allow bool) (string, error)
	// AskQuestions 返回某个待回答 ask 的问题列表（用于把 IM 文本解析成选项）。
	AskQuestions(askID string) ([]event.AskQuestion, bool)
	// Answer 应答任意桌面会话的待回答 ask，返回用户可读的结果文案。
	Answer(askID string, answers []event.AskAnswer) (string, error)
	// Takeover 把该聊天绑定为某个桌面会话的远程驾驶者，返回用户可读文案。
	Takeover(route DesktopWatchRoute, tabID string) (string, error)
	// Release 解除该聊天的接管绑定，返回用户可读文案。
	Release(route DesktopWatchRoute) (string, error)
	// TakeoverTab 返回该聊天当前接管的 tabID，未接管时为空串。
	TakeoverTab(route DesktopWatchRoute) string
	// DriveInput 把一条文本作为新 turn 提交到该聊天接管的桌面会话。
	// 成功时输出会经事件转发流回聊天；返回的文案（可为空）用于即时回执。
	DriveInput(route DesktopWatchRoute, text string) (string, error)
}

func desktopRouteFromMessage(msg InboundMessage) DesktopWatchRoute {
	return DesktopWatchRoute{
		ConnectionID: msg.ConnectionID,
		Domain:       msg.Domain,
		Platform:     msg.Platform,
		ChatType:     msg.ChatType,
		ChatID:       msg.ChatID,
	}
}

const desktopCommandUsage = "用法:\n" +
	"/desktop status - 查看桌面端所有 live 会话\n" +
	"/desktop watch on|off|status - 订阅/退订桌面事件推送(审批请求、任务完成/出错)\n" +
	"/desktop approve <id> - 批准桌面会话的待审批操作\n" +
	"/desktop deny <id> - 拒绝桌面会话的待审批操作\n" +
	"/desktop answer <id> <选项编号或文本> - 回答桌面会话的提问\n" +
	"/desktop takeover <tab> - 接管桌面会话，后续消息直接驱动它\n" +
	"/desktop release - 解除接管，回到普通 bot 会话"

// handleDesktopCommand 处理 /desktop 系列命令(上帝视角：观察 + 遥控审批)。
// 调用方已完成 admin 角色门控。
func (gw *BotGateway) handleDesktopCommand(msg InboundMessage) string {
	bridge := gw.cfg.Desktop
	if bridge == nil {
		return "此 bot 未运行在桌面端进程内，/desktop 命令不可用。请在桌面端设置里启用 bot。"
	}
	fields := strings.Fields(msg.Text)
	sub := ""
	if len(fields) > 1 {
		sub = strings.ToLower(fields[1])
	}
	switch sub {
	case "", "status", "sessions":
		return formatDesktopSessions(bridge.Sessions())
	case "watch":
		arg := ""
		if len(fields) > 2 {
			arg = strings.ToLower(fields[2])
		}
		route := desktopRouteFromMessage(msg)
		switch arg {
		case "on":
			if err := bridge.SetWatch(route, true); err != nil {
				return "已在本次运行中订阅桌面事件，但保存订阅失败；桌面端重启后可能需要重新订阅。"
			}
			return "已订阅桌面事件：审批请求、任务完成/出错会推送到本聊天。订阅已保存，桌面端重启后仍会生效。用 /desktop watch off 退订。"
		case "off":
			if err := bridge.SetWatch(route, false); err != nil {
				return "已在本次运行中退订桌面事件，但保存失败；桌面端重启后订阅可能恢复。"
			}
			return "已退订桌面事件推送，持久化订阅也已移除。"
		case "", "state":
			if bridge.Watching(route) {
				return "本聊天正在订阅桌面事件推送。用 /desktop watch off 退订。"
			}
			return "本聊天未订阅桌面事件推送。用 /desktop watch on 订阅。"
		default:
			return desktopCommandUsage
		}
	case "approve", "deny":
		if len(fields) < 3 {
			return desktopCommandUsage
		}
		feedback, err := bridge.Approve(fields[2], sub == "approve")
		if err != nil {
			return err.Error()
		}
		return feedback
	case "answer":
		if len(fields) < 4 {
			return desktopCommandUsage
		}
		askID := fields[2]
		questions, ok := bridge.AskQuestions(askID)
		if !ok {
			return fmt.Sprintf("未找到待回答的提问 %s（可能已在桌面端回答或已超时）。", askID)
		}
		raw := strings.Join(fields[3:], " ")
		answers := parseAskAnswers(questions, raw)
		feedback, err := bridge.Answer(askID, answers)
		if err != nil {
			return err.Error()
		}
		return feedback
	case "takeover":
		if len(fields) < 3 {
			return desktopCommandUsage
		}
		feedback, err := bridge.Takeover(desktopRouteFromMessage(msg), fields[2])
		if err != nil {
			return err.Error()
		}
		return feedback
	case "release":
		feedback, err := bridge.Release(desktopRouteFromMessage(msg))
		if err != nil {
			return err.Error()
		}
		return feedback
	default:
		return desktopCommandUsage
	}
}

// divertToDesktopTakeover 把已接管聊天的普通消息改道到桌面会话。斜杠命令
// 不经过这里（仍走 handleSlashCommand），所以 /desktop release 永远可用。
// 返回 true 表示消息已被桌面接管通道消费。
func (gw *BotGateway) divertToDesktopTakeover(ctx context.Context, adapter Adapter, msg InboundMessage) bool {
	if strings.HasPrefix(strings.TrimSpace(msg.Text), "/") {
		return false
	}
	bridge := gw.cfg.Desktop
	if bridge == nil {
		return false
	}
	route := desktopRouteFromMessage(msg)
	if bridge.TakeoverTab(route) == "" {
		return false
	}
	// Re-check the continuing capability, not only the command that created it.
	// A runtime refresh can revoke admin while leaving this process-local binding
	// alive; base allowlist admission alone must not preserve takeover power.
	if !gw.checkCommandRole(msg.Platform, msg, "admin") {
		_, _ = bridge.Release(route)
		_ = gw.sendText(ctx, adapter, msg, "桌面接管已解除：当前账号不再具有 bot 管理员权限。")
		return true
	}
	if strings.TrimSpace(msg.Text) == "" {
		_ = gw.sendText(ctx, adapter, msg, "接管模式暂不支持转发附件，请发送文本。")
		return true
	}
	feedback, err := bridge.DriveInput(route, msg.Text)
	if err != nil {
		_ = gw.sendText(ctx, adapter, msg, err.Error())
		return true
	}
	if strings.TrimSpace(feedback) != "" {
		_ = gw.sendText(ctx, adapter, msg, feedback)
	}
	return true
}

func formatDesktopSessions(sessions []DesktopSessionInfo) string {
	if len(sessions) == 0 {
		return "桌面端当前没有 live 会话。"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "桌面端 live 会话（%d 个）:\n", len(sessions))
	for _, s := range sessions {
		state := "空闲"
		switch {
		case s.PendingPrompt:
			state = "⚠️ 等待审批/回答"
		case s.Running:
			state = "▶️ 执行中"
		case !s.Ready:
			state = "启动中"
		}
		label := strings.TrimSpace(s.Label)
		if label == "" {
			label = strings.TrimSpace(s.Topic)
		}
		if label == "" {
			label = "(未命名)"
		}
		if s.Detached {
			state += "·后台"
		}
		fmt.Fprintf(&b, "\n- %s [%s]", label, state)
		if ws := strings.TrimSpace(s.Workspace); ws != "" {
			fmt.Fprintf(&b, "\n  项目: %s", ws)
		}
		fmt.Fprintf(&b, "\n  tab: %s", s.TabID)
		for _, p := range s.Pending {
			kind := "审批"
			if p.Kind == "ask" {
				kind = "提问"
			}
			line := fmt.Sprintf("\n  待%s: %s", kind, p.ID)
			if tool := strings.TrimSpace(p.Tool); tool != "" {
				line += " (" + tool + ")"
			}
			b.WriteString(line)
		}
	}
	b.WriteString("\n\n用 /desktop watch on 订阅审批与完成事件；/desktop takeover <tab> 接管某个会话。")
	return b.String()
}
