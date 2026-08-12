package main

// 桌面语音转文字（STT）的 Wails 绑定方法：输入框麦克风按钮与设置开关
// 通过这里控制 sttBridge。前端对应 lib/bridge.ts 中的 STTStart/STTStop/
// STTStatus/STTSetLang 与 SetDesktopSTTEnabled。

import (
	"fmt"
	"strings"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"reasonix/internal/config"
)

// sttBridgeFor 返回（并惰性创建）当前 App 的 STT 桥接服务。转录事件经
// runtime.EventsEmit 推给前端（lib/bridge.ts onSTTTranscript 订阅）。
func (a *App) sttBridgeFor() *sttBridge {
	if a.stt == nil {
		a.stt = newSTTBridge(config.ReasonixHomeDir(), func(name string, data ...interface{}) {
			if a.ctx != nil {
				runtime.EventsEmit(a.ctx, name, data...)
			}
		})
	}
	return a.stt
}

// stopSTTOnClose 在应用退出前停止识别、关闭本地服务并终止 Edge 进程。
func (a *App) stopSTTOnClose() {
	if a.stt != nil {
		a.stt.Stop()
	}
}

// STTStart 开始语音识别：惰性启动本地服务与 Edge 识别页，等待浏览器连上后
// 自动开始录音。首次点击时 Edge 页面加载需要几秒，StartWithWait 会等待连接，
// 一次点击即生效（无需再点第二次）。tabID 为发起识别的标签页，转录事件
// 携带它，前端据此把转录插入正确的窗口输入框（多窗口独立绑定不错乱）。
func (a *App) STTStart(tabID string) error {
	if !a.desktopSTTEnabled() {
		return fmt.Errorf("语音转文字未启用，请在设置中开启")
	}
	// 每次启动都同步一次设置（识别页显示/自动停止/快捷键），
	// 保证设置面板的修改即时生效。
	a.applySTTSettingsToBridge()
	b := a.sttBridgeFor()
	b.mu.Lock()
	b.tabID = tabID
	b.mu.Unlock()
	return b.StartWithWait(0)
}

// STTStop 停止语音识别（浏览器停止录音；本地服务与 Edge 保持，便于再次开始）。
func (a *App) STTStop() error {
	if a.stt == nil {
		return nil
	}
	return a.stt.StopListening()
}

// STTStatus 返回当前识别状态（供前端按钮态/诊断）。
func (a *App) STTStatus() map[string]any {
	if a.stt == nil {
		return map[string]any{"running": false, "listening": false, "connected": false}
	}
	return a.stt.Status()
}

// STTSetLang 切换识别语言（zh-CN / en-US 等）。空值回退 zh-CN。
func (a *App) STTSetLang(lang string) error {
	if a.stt == nil {
		return nil
	}
	return a.stt.SetLang(lang)
}

// SetDesktopSTTEnabled 持久化设置开关（[desktop] stt_enabled）。关闭时立即
// 停止识别并清理 Edge；开启只写配置，首次点击麦克风按钮时才拉起服务。
func (a *App) SetDesktopSTTEnabled(enabled bool) error {
	err := a.applyConfigOnly(func(c *config.Config) error {
		return c.SetDesktopSTTEnabled(enabled)
	})
	if err != nil {
		return err
	}
	if !enabled {
		a.stopSTTOnClose()
	} else {
		a.applySTTSettingsToBridge()
	}
	return nil
}

// applySTTSettingsToBridge 把当前配置的语音输入选项同步到 sttBridge
// （识别页显示/自动停止/快捷键），使设置即时生效。
// 惰性创建 bridge：应用重启后 a.stt 为空，但热键是启动服务的入口，
// 必须在此处先创建 bridge 并注册热键，否则按热键无效。
func (a *App) applySTTSettingsToBridge() {
	cfg, _, err := a.loadDesktopUserConfigForView()
	if err != nil {
		return
	}
	b := a.sttBridgeFor()
	b.SetOptions(
		cfg.Desktop.STTShowPage,
		cfg.Desktop.STTAutoStop,
		cfg.Desktop.STTAutoStopSeconds,
		cfg.Desktop.STTHotkeyStart,
		cfg.Desktop.STTHotkeyStop,
	)
}

// SetDesktopSTTShowPage 持久化"识别页是否显示"（[desktop] stt_show_page）。
// false = Edge 识别页后台运行（窗口隐藏）。
func (a *App) SetDesktopSTTShowPage(show bool) error {
	err := a.applyConfigOnly(func(c *config.Config) error {
		return c.SetDesktopSTTShowPage(show)
	})
	if err != nil {
		return err
	}
	a.applySTTSettingsToBridge()
	return nil
}

// SetDesktopSTTAutoStop 持久化"不说话自动停止"（[desktop] stt_auto_stop）。
func (a *App) SetDesktopSTTAutoStop(enabled bool) error {
	err := a.applyConfigOnly(func(c *config.Config) error {
		return c.SetDesktopSTTAutoStop(enabled)
	})
	if err != nil {
		return err
	}
	a.applySTTSettingsToBridge()
	return nil
}

// SetDesktopSTTAutoStopOnSwitch 持久化"切换对话窗口时自动停止识别"
// （[desktop] stt_auto_stop_on_switch）。
func (a *App) SetDesktopSTTAutoStopOnSwitch(enabled bool) error {
	err := a.applyConfigOnly(func(c *config.Config) error {
		return c.SetDesktopSTTAutoStopOnSwitch(enabled)
	})
	if err != nil {
		return err
	}
	a.applySTTSettingsToBridge()
	return nil
}

// SetDesktopSTTAutoStopSeconds 持久化静默超时秒数（[desktop] stt_auto_stop_seconds）。
func (a *App) SetDesktopSTTAutoStopSeconds(seconds int) error {
	err := a.applyConfigOnly(func(c *config.Config) error {
		return c.SetDesktopSTTAutoStopSeconds(seconds)
	})
	if err != nil {
		return err
	}
	a.applySTTSettingsToBridge()
	return nil
}

// SetDesktopSTTHotkeyStart 持久化开始识别的全局快捷键（空串禁用）。
func (a *App) SetDesktopSTTHotkeyStart(hotkey string) error {
	err := a.applyConfigOnly(func(c *config.Config) error {
		return c.SetDesktopSTTHotkeyStart(hotkey)
	})
	if err != nil {
		return err
	}
	a.applySTTSettingsToBridge()
	return nil
}

// SetDesktopSTTHotkeyStop 持久化停止识别的全局快捷键（空串禁用）。
func (a *App) SetDesktopSTTHotkeyStop(hotkey string) error {
	err := a.applyConfigOnly(func(c *config.Config) error {
		return c.SetDesktopSTTHotkeyStop(hotkey)
	})
	if err != nil {
		return err
	}
	a.applySTTSettingsToBridge()
	return nil
}

// desktopSTTEnabled 读取当前设置开关（默认关闭）。
func (a *App) desktopSTTEnabled() bool {
	cfg, _, err := a.loadDesktopUserConfigForView()
	if err != nil {
		return false
	}
	return cfg.Desktop.STTEnabled
}

// normalizeSTTLang 归一化识别语言，空值/非法值回退 zh-CN。
func normalizeSTTLang(lang string) string {
	l := strings.ToLower(strings.TrimSpace(lang))
	if l == "" || l == "zh" || strings.HasPrefix(l, "zh-") {
		return "zh-CN"
	}
	if strings.HasPrefix(l, "en") {
		return "en-US"
	}
	return strings.TrimSpace(lang)
}
