package bot

import (
	"context"
	"strings"

	"reasonix/internal/sandbox"
)

func (gw *BotGateway) normalizeApprovalShortcut(key, text string) (string, bool) {
	id := gw.currentPendingApprovalID(key)
	if id == "" {
		return "", false
	}
	if gw.pendingApprovalIsRecovery(key, id) {
		command, ok := recoveryShortcutCommand(text, gw.pendingRecoveryCanGrantTask(key, id))
		return shortcutWithID(command, id, ok)
	}
	if gw.pendingApprovalIsWriteAccess(key, id) {
		command, ok := writeAccessShortcutCommand(text)
		return shortcutWithID(command, id, ok)
	}
	command, ok := approvalShortcutCommand(text)
	return shortcutWithID(command, id, ok)
}

func shortcutWithID(command, id string, ok bool) (string, bool) {
	if !ok {
		return "", false
	}
	return command + " " + id, true
}

func writeAccessShortcutCommand(text string) (string, bool) {
	switch strings.ToLower(strings.TrimSpace(text)) {
	case "1", "y", "yes", "ok", "同意", "批准", "允许", "允许一次":
		return "/approve", true
	case "2", "a", "session", "本会话", "本会话允许":
		return "/approve-session", true
	case "3", "p", "project", "加入项目":
		return "/approve-project", true
	case "4", "0", "n", "no", "deny", "拒绝":
		return "/deny", true
	default:
		return "", false
	}
}

func (gw *BotGateway) pendingApprovalIsWriteAccess(key, id string) bool {
	gw.mu.Lock()
	defer gw.mu.Unlock()
	state, ok := gw.controllers[key]
	if !ok || state.pendingApprovals == nil {
		return false
	}
	a, ok := state.pendingApprovals[id]
	return ok && isWriteAccessApproval(a)
}

func (gw *BotGateway) handleSlashCommand(ctx context.Context, adapter Adapter, key string, msg InboundMessage) {
	if gw.handleWriteAccessSlashCommand(ctx, adapter, msg, key) {
		return
	}
	gw.handleSlashCommandCore(ctx, adapter, key, msg)
}

func (gw *BotGateway) handleWriteAccessSlashCommand(ctx context.Context, adapter Adapter, msg InboundMessage, key string) bool {
	switch {
	case strings.HasPrefix(msg.Text, "/approve-session"):
		gw.handleScopedApprove(ctx, adapter, msg, key, sandbox.ApprovalScopeSession, "用法: /approve-session <id>", "已批准本会话写入范围。")
		return true
	case strings.HasPrefix(msg.Text, "/approve-project"):
		gw.handleScopedApprove(ctx, adapter, msg, key, sandbox.ApprovalScopeProject, "用法: /approve-project <id>", "已写入项目允许目录。")
		return true
	default:
		return false
	}
}

func (gw *BotGateway) handleScopedApprove(ctx context.Context, adapter Adapter, msg InboundMessage, key string, scope sandbox.ApprovalScope, usage, okText string) {
	if !gw.requireCommandRole(ctx, adapter, msg, "approver") {
		return
	}
	parts := strings.Fields(msg.Text)
	if len(parts) < 2 {
		_ = gw.sendText(ctx, adapter, msg, usage)
		return
	}
	gw.mu.Lock()
	state, ok := gw.controllers[key]
	gw.mu.Unlock()
	if !ok || state.ctrl == nil {
		_ = gw.sendText(ctx, adapter, msg, "没有找到当前会话中的待审批操作，请重新触发一次操作。")
		return
	}
	if gw.pendingApprovalIsRecovery(key, parts[1]) {
		_ = gw.sendText(ctx, adapter, msg, "恢复确认不支持会话/项目写入授权，请使用继续或换个办法。")
		return
	}
	err := state.ctrl.ResolveApproval(parts[1], true, scope)
	gw.forgetPendingApproval(key, parts[1])
	if err != nil {
		_ = gw.sendText(ctx, adapter, msg, "保存失败: "+err.Error())
		return
	}
	_ = gw.sendText(ctx, adapter, msg, okText)
}

func botHelpText() string {
	return "可用命令:\n" +
		"/stop - 停止当前任务\n/new - 开始新会话\n/reset - 重置会话\n" +
		"/approve <id> - 批准操作（仅本次）\n/approve-session <id> - 本会话允许这些目录\n" +
		"/approve-project <id> - 加入项目允许目录\n/deny <id> - 拒绝操作\n" +
		"/answer <id> <选项> - 回答 ask 问题\n/yolo on|off|auto|status - 切换或查看工具审批模式\n" +
		"/mode yolo|ask|auto - 切换工具审批模式\n/queue steer|followup|collect|interrupt|status - 切换或查看队列模式\n" +
		"/projects [关键词] - 查看可切换项目索引\n/use project <id|名称> - 将当前远端会话切到某个项目\n" +
		"/sessions search <关键词> - 搜索可 attach 的历史会话\n/attach session <id|关键词> - 绑定当前远端会话到已有历史会话\n" +
		"/search all <关键词> - 跨已索引项目检索文件内容\n" +
		"/desktop status|watch|approve|deny|answer - 桌面端上帝视角(需内嵌运行)\n" +
		"/status - 查看状态\n/help - 显示帮助"
}
