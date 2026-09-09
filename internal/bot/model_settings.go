package bot

import (
	"context"
	"fmt"
	"strings"

	"reasonix/internal/boot"
	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/secrets"
)

// The session queue owns this new turn. Approval replies and child work never
// enter here. Keep the previous state and lease until the complete replacement
// is ready, then fence publication against retirement and a newer disk save.
func (gw *BotGateway) applySessionModelSettings(ctx context.Context, key string, msg InboundMessage, previous *sessionState) (*sessionState, error) {
	old, ok := previous.ctrl.(*control.Controller)
	if !ok {
		return previous, nil
	}
	previous.lifecycleMu.Lock()
	defer previous.lifecycleMu.Unlock()
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if previous.retired {
			return nil, errBotSessionRetired
		}
		applied, desired, err := old.ModelSettingsState()
		if err != nil {
			return nil, err
		}
		if applied == desired {
			return previous, nil
		}
		if botSessionHasActiveWork(previous) {
			return nil, fmt.Errorf("saved model settings are pending until current work finishes")
		}
		cfg, err := config.LoadModelRuntimeSnapshot(previous.workspaceRoot, old.ModelRef())
		if err != nil {
			return nil, err
		}
		model := old.ModelRef()
		if entry, ok := cfg.ResolveModel(model); !ok || !entry.Configured() {
			var available bool
			model, _, available = cfg.ResolveNewSessionChatModel()
			if !available || strings.TrimSpace(model) == "" {
				return nil, fmt.Errorf("choose a configured model before starting another bot request")
			}
		}
		next := &sessionState{
			sink: &sessionEventSink{}, leases: previous.leases,
			platform: previous.platform, connectionID: previous.connectionID,
			model: previous.model, workspaceRoot: previous.workspaceRoot,
			toolApprovalMode: previous.toolApprovalMode, sessionPath: previous.sessionPath,
			mappingDegraded: previous.mappingDegraded, createdAt: previous.createdAt, lastActive: previous.lastActive,
			pendingApprovals: map[string]event.Approval{}, pendingAsks: map[string][]event.AskQuestion{},
		}
		next.onSessionTransition = gw.botSessionTransitionHandler(key, msg, next)
		result, err := boot.Rebuild(ctx, old, boot.Options{
			Model: model, RequireKey: true, RuntimeReload: boot.RuntimeReload{ForceFullRebuild: true},
			MaxSteps: gw.cfg.MaxSteps, MaxStepsKey: "bot.max_steps", Sink: next.sink,
			StatsSource: "bot", WorkspaceRoot: next.workspaceRoot, SessionDir: botSessionDir(next.workspaceRoot),
			ApprovalTimeout:    gw.approvalTimeout(),
			OnSessionRecovered: gw.botSessionRecoveredHandler(key, msg, next), OnSessionTransition: next.onSessionTransition,
		})
		if err != nil {
			return nil, err
		}
		next.ctrl = result.Controller
		result.Controller.EnableInteractiveApproval()
		a, d, err := result.Controller.ModelSettingsState()
		if err != nil || a != d {
			result.Controller.Close()
			if err != nil {
				return nil, err
			}
			continue
		}
		if err := bindBotSessionWriteAuthority(next); err != nil {
			result.Controller.Close()
			_ = bindBotSessionWriteAuthority(previous)
			return nil, err
		}
		gw.mu.Lock()
		if gw.controllers[key] != previous {
			gw.mu.Unlock()
			result.Controller.Close()
			_ = bindBotSessionWriteAuthority(previous)
			return nil, fmt.Errorf("bot session changed while applying saved model settings")
		}
		gw.controllers[key] = next
		previous.leases = nil
		previous.retired = true
		gw.mu.Unlock()
		old.Close()
		return next, nil
	}
}

func (gw *BotGateway) sessionForNewTurn(ctx context.Context, adapter Adapter, key string, msg InboundMessage) *sessionState {
	// 获取或创建 Controller
	state := gw.getOrCreateSession(ctx, key, msg)
	if state == nil || state.ctrl == nil {
		_ = gw.sendText(ctx, adapter, msg, "内部错误：无法创建会话。")
		return nil
	}
	var settingsErr error
	state, settingsErr = gw.applySessionModelSettings(ctx, key, msg, state)
	if settingsErr != nil {
		gw.logger.Warn("bot model settings application failed", "err", secrets.RedactError(settingsErr))
		_ = gw.sendText(ctx, adapter, msg, "模型设置已保存，但当前会话尚未成功应用。请检查可用模型后重试；原会话仍保留。")
		return nil
	}
	return state
}
