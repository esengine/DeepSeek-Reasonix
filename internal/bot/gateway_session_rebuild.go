package bot

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"reasonix/internal/agent"
	"reasonix/internal/boot"
	"reasonix/internal/control"
	"reasonix/internal/event"
	"reasonix/internal/secrets"
)

type builtBotSession struct {
	state       *sessionState
	reusedLease bool
}

func botRuntimeSwitchBusyText() string {
	return "当前会话仍有正在运行、等待确认或后台执行的任务。请先完成或停止这些任务，再切换项目或 attach 会话。"
}

func botRuntimeSwitchFailedText(action string) string {
	return action + "失败，当前会话保持不变。请检查配置后重试。"
}

func (gw *BotGateway) buildBotController(ctx context.Context, opts boot.Options) (*control.Controller, error) {
	if gw.buildController != nil {
		return gw.buildController(ctx, opts)
	}
	return boot.Build(ctx, opts)
}

// buildSessionState prepares a complete replacement without publishing it.
// When the transcript path is unchanged, the candidate reuses the old keeper
// so the session lease never has an unowned window during a model/profile swap.
func (gw *BotGateway) buildSessionState(ctx context.Context, key string, msg InboundMessage, profile sessionRuntimeProfile, previous *sessionState) (*builtBotSession, error) {
	leases := control.NewSessionLeaseKeeper()
	reusedLease := false
	if previous != nil && previous.leases != nil {
		heldPath := agent.CanonicalSessionPath(previous.leases.HeldPath())
		if heldPath != "" && heldPath == agent.CanonicalSessionPath(profile.sessionPath) {
			leases = previous.leases
			reusedLease = true
		}
	}

	sessionSink := &sessionEventSink{}
	state := &sessionState{
		sink:             sessionSink,
		leases:           leases,
		platform:         msg.Platform,
		connectionID:     strings.TrimSpace(msg.ConnectionID),
		model:            profile.model,
		workspaceRoot:    profile.workspaceRoot,
		toolApprovalMode: profile.toolApprovalMode,
		sessionPath:      profile.sessionPath,
		pendingAsks:      make(map[string][]event.AskQuestion),
		createdAt:        time.Now(),
		lastActive:       time.Now(),
	}
	state.onSessionTransition = gw.botSessionTransitionHandler(key, msg, state)
	ctrl, err := gw.buildBotController(ctx, boot.Options{
		Model:               profile.model,
		MaxSteps:            gw.cfg.MaxSteps,
		MaxStepsKey:         "bot.max_steps",
		RequireKey:          true,
		Sink:                sessionSink,
		StatsSource:         "bot",
		WorkspaceRoot:       profile.workspaceRoot,
		SessionDir:          botSessionDir(profile.workspaceRoot),
		ApprovalTimeout:     gw.approvalTimeout(),
		OnSessionRecovered:  gw.botSessionRecoveredHandler(key, msg, state),
		OnSessionTransition: state.onSessionTransition,
	})
	if err != nil {
		if !reusedLease {
			leases.Release()
		}
		return nil, err
	}
	state.ctrl = ctrl
	fail := func(buildErr error) (*builtBotSession, error) {
		ctrl.Close()
		if reusedLease {
			if restoreErr := bindBotSessionWriteAuthority(previous); restoreErr != nil {
				gw.logger.Error("restore bot session write authority failed", "err", secrets.RedactError(restoreErr))
			}
		} else {
			leases.Release()
		}
		return nil, buildErr
	}

	if profile.sessionPath != "" {
		degrade := func(reason string, loadErr error) bool {
			if !profile.sessionPathOptional {
				return false
			}
			gw.logger.Warn("mapped bot session unavailable; starting fresh", "reason", reason, "session_path", profile.sessionPath, "err", loadErr)
			profile.sessionPath = ""
			state.sessionPath = ""
			state.mappingDegraded = true
			return true
		}
		if err := leases.Rebind(profile.sessionPath); err != nil {
			if !degrade("lease held elsewhere", err) {
				return fail(fmt.Errorf("attached bot session is in use: %w", err))
			}
		} else if loaded, err := agent.LoadSession(profile.sessionPath); err != nil {
			if os.IsNotExist(err) && profile.sessionPathOptional {
				ctrl.SetSessionPath(profile.sessionPath)
			} else if !degrade("load failed", err) {
				return fail(fmt.Errorf("load attached bot session: %w", err))
			}
		} else {
			ctrl.Resume(loaded, profile.sessionPath)
		}
	}
	ctrl.EnableInteractiveApproval()
	ctrl.SetToolApprovalMode(profile.toolApprovalMode)
	ctrl.EnsureSessionPath()
	if reusedLease && agent.CanonicalSessionPath(ctrl.SessionPath()) != agent.CanonicalSessionPath(leases.HeldPath()) {
		return fail(errors.New("replacement session path changed while reusing the current lease"))
	}
	if err := rebindBotSessionWriteAuthority(state, ctrl.SessionPath()); err != nil {
		return fail(fmt.Errorf("bind bot session write authority: %w", err))
	}
	return &builtBotSession{state: state, reusedLease: reusedLease}, nil
}

func (gw *BotGateway) discardBuiltSession(built *builtBotSession, previous *sessionState) {
	if built == nil || built.state == nil {
		return
	}
	if built.state.ctrl != nil {
		built.state.ctrl.Close()
	}
	if built.reusedLease {
		if previous != nil {
			previous.lifecycleMu.Lock()
			retired := previous.retired
			previous.lifecycleMu.Unlock()
			if !retired {
				if err := bindBotSessionWriteAuthority(previous); err != nil {
					gw.logger.Error("restore bot session write authority failed", "err", secrets.RedactError(err))
				}
			}
		}
		return
	}
	if built.state.leases != nil {
		built.state.leases.Release()
	}
}

func (gw *BotGateway) setSessionRuntimeOverride(ctx context.Context, key string, msg InboundMessage, override sessionRuntimeOverride, enabled bool) (bool, error) {
	override.sessionPath = canonicalBotPath(override.sessionPath)
	override.channel.WorkspaceRoot = canonicalBotPath(override.channel.WorkspaceRoot)
	profile := gw.sessionProfileForResolvedOverride(msg, override, enabled)
	var switchErr error
	switched := gw.sessions.runIfIdle(key, func() bool {
		gw.mu.Lock()
		previous := gw.controllers[key]
		if previous == nil {
			if enabled {
				gw.sessionOverrides[key] = override
			} else {
				delete(gw.sessionOverrides, key)
			}
			gw.mu.Unlock()
			return true
		}
		if previous != nil && botSessionHasActiveWork(previous) {
			gw.mu.Unlock()
			return false
		}
		if previous != nil && sessionStateMatchesRuntime(previous, profile) {
			if enabled {
				gw.sessionOverrides[key] = override
			} else {
				delete(gw.sessionOverrides, key)
			}
			updateSessionStateRuntime(previous, msg, profile)
			gw.mu.Unlock()
			safeBotSetToolApprovalMode(previous.ctrl, profile.toolApprovalMode)
			return true
		}
		gw.mu.Unlock()

		built, err := gw.buildSessionState(ctx, key, msg, profile, previous)
		if err != nil {
			switchErr = err
			gw.logger.Error("bot session runtime switch failed", "err", secrets.RedactError(err))
			return false
		}

		gw.mu.Lock()
		if gw.controllers[key] != previous {
			gw.mu.Unlock()
			gw.discardBuiltSession(built, previous)
			switchErr = errors.New("bot session changed while replacement was building")
			return false
		}
		if enabled {
			gw.sessionOverrides[key] = override
		} else {
			delete(gw.sessionOverrides, key)
		}
		gw.controllers[key] = built.state
		if built.reusedLease && previous != nil {
			previous.leases = nil
		}
		gw.mu.Unlock()
		gw.closeSessionState(previous)
		return true
	})
	return switched, switchErr
}
