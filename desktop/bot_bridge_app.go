package main

import (
	"fmt"
	"log/slog"
	"strings"

	"reasonix/internal/bot"
	"reasonix/internal/config"
	"reasonix/internal/control"
	"reasonix/internal/event"
)

// 本文件是 botBridgeHub 对 App 的全部胶水：会话枚举（含后台 detached）、
// 按 tab 寻址的审批/问答/驱动、transcript 公告、订阅持久化。

func (a *App) newBotBridge() *botBridgeHub {
	return newBotBridgeHub(botBridgeDeps{
		sessions:        a.bridgeSessions,
		approveTab:      a.bridgeApprove,
		answerTab:       a.bridgeAnswer,
		notify:          a.botRuntime.SendToAdapter,
		drive:           a.bridgeDrive,
		announce:        a.bridgeAnnounce,
		persistWatchers: a.bridgePersistWatchers,
		takeoverChanged: a.emitProjectTreeChanged,
		logger:          slog.Default(),
	})
}

// bridgeSessions 枚举所有 live 会话：可见 tab 用完整 TabMeta，后台 detached
// 会话补一份轻量快照（controller 仍存活，审批/问答仍可路由）。
func (a *App) bridgeSessions() []bot.DesktopSessionInfo {
	tabs := a.ListTabs()
	out := make([]bot.DesktopSessionInfo, 0, len(tabs)+4)
	seen := make(map[string]bool, len(tabs))
	for _, t := range tabs {
		seen[t.ID] = true
		out = append(out, bot.DesktopSessionInfo{
			TabID:         t.ID,
			Label:         t.Label,
			Workspace:     t.WorkspaceName,
			Topic:         t.TopicTitle,
			Ready:         t.Ready,
			Running:       t.Running,
			PendingPrompt: t.PendingPrompt,
		})
	}
	a.mu.RLock()
	for _, tab := range a.detachedSessions {
		if tab == nil || seen[tab.ID] {
			continue
		}
		seen[tab.ID] = true
		out = append(out, bot.DesktopSessionInfo{
			TabID:    tab.ID,
			Label:    tab.TopicTitle,
			Topic:    tab.TopicTitle,
			Ready:    tab.Ctrl != nil,
			Running:  strings.TrimSpace(tab.ActivityStatus) != "",
			Detached: true,
		})
	}
	a.mu.RUnlock()
	return out
}

// bridgeCtrlByTabID 解析可见与后台 detached 两张表（区别于 ctrlByTabID：
// 那是前端语义，空 tabID 落到活跃 tab，且不看 detached）。
func (a *App) bridgeCtrlByTabID(tabID string) control.SessionAPI {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if tab := a.tabByEventSinkIDLocked(tabID); tab != nil {
		return tab.Ctrl
	}
	return nil
}

func (a *App) bridgeApprove(tabID, id string, allow, session, persist bool) {
	if ctrl := a.bridgeCtrlByTabID(tabID); ctrl != nil {
		ctrl.Approve(id, allow, session, persist)
	}
}

func (a *App) bridgeAnswer(tabID, id string, answers []QuestionAnswer) {
	ctrl := a.bridgeCtrlByTabID(tabID)
	if ctrl == nil {
		return
	}
	out := make([]event.AskAnswer, len(answers))
	for i, an := range answers {
		out[i] = event.AskAnswer{QuestionID: an.QuestionID, Selected: an.Selected}
	}
	ctrl.AnswerQuestion(id, out)
}

// bridgeAnnounce 往会话 transcript 发一条 Notice，桌面用户在聊天流里可见。
func (a *App) bridgeAnnounce(tabID, text string) {
	a.mu.RLock()
	tab := a.tabByEventSinkIDLocked(tabID)
	var sink *tabEventSink
	if tab != nil {
		sink = tab.sink
	}
	a.mu.RUnlock()
	if sink == nil {
		return
	}
	sink.Emit(event.Event{Kind: event.Notice, Level: event.LevelWarn, Text: text})
}

// bridgeDrive 把远程文本提交为可见 tab 的新 turn，并为这一轮挂上事件转发器,
// 让输出流回接管聊天（转发器在 TurnDone 自动卸载）。
func (a *App) bridgeDrive(tabID, text string, route bot.DesktopWatchRoute) error {
	tab, ctrl, err := a.beginTabTurn(tabID, false)
	if err != nil {
		if err == control.ErrTurnRunning {
			return errDriveBusy
		}
		return err
	}
	if tab.sink == nil {
		a.finishTabTurnStart(tab, nil)
		return fmt.Errorf("会话事件通道不可用，无法驱动")
	}
	// A local submission may have reclaimed the tab while this drive was waiting
	// for the per-tab admission gate. Revalidate ownership only after the gate is
	// held, immediately before attaching the route-specific forwarder.
	if a.botBridge == nil || a.botBridge.TakeoverTab(route) != tabID {
		tab.sink.cancelTurnStart()
		tab.turnStartMu.Unlock()
		return fmt.Errorf("接管已解除，请重新接管会话")
	}
	target := botForwardTarget{
		ConnID:   route.ConnectionID,
		Domain:   route.Domain,
		ChatID:   route.ChatID,
		ChatType: route.ChatType,
	}
	generation := tab.sink.SetBotSink(newBotEventForwarder(a.botRuntime, []botForwardTarget{target}))
	a.ensureTabTopicIndexedForUserTurn(tab)
	ctrl.SubmitDisplay(text, text)
	// Confirm the submit actually started a turn. If nothing is running now, the
	// controller was rotating and the submit no-oped — detach this exact
	// generation so a later turn's output does not leak.
	if !a.finishTabTurnStart(tab, ctrl) {
		tab.sink.clearBotSink(generation)
		return errDriveBusy
	}
	return nil
}

// bridgePersistWatchers 把订阅全集回写用户配置（bot.desktop_watchers），
// 桌面重启后由 refreshBotRuntime 重新种子。
func (a *App) bridgePersistWatchers(routes []bot.DesktopWatchRoute) error {
	return a.applyConfigOnly(func(c *config.Config) error {
		watchers := make([]config.BotDesktopWatcherConfig, 0, len(routes))
		for _, r := range routes {
			watchers = append(watchers, config.BotDesktopWatcherConfig{
				Platform:     string(r.Platform),
				ConnectionID: r.ConnectionID,
				Domain:       r.Domain,
				ChatType:     string(r.ChatType),
				ChatID:       r.ChatID,
			})
		}
		c.Bot.DesktopWatchers = watchers
		return nil
	})
}

func bridgeRoutesFromConfig(watchers []config.BotDesktopWatcherConfig) []bot.DesktopWatchRoute {
	routes := make([]bot.DesktopWatchRoute, 0, len(watchers))
	for _, w := range watchers {
		routes = append(routes, bot.DesktopWatchRoute{
			Platform:     bot.Platform(strings.TrimSpace(w.Platform)),
			ConnectionID: strings.TrimSpace(w.ConnectionID),
			Domain:       strings.TrimSpace(w.Domain),
			ChatType:     bot.ChatType(strings.TrimSpace(w.ChatType)),
			ChatID:       strings.TrimSpace(w.ChatID),
		})
	}
	return routes
}
