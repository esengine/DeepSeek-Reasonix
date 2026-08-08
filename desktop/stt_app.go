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

// STTStart 开始语音识别：惰性启动本地服务与 Edge 识别页，再向浏览器发送
// 开始命令。返回错误时前端用 toast 提示（如 Edge 未安装/未连接）。
func (a *App) STTStart() error {
	if !a.desktopSTTEnabled() {
		return fmt.Errorf("语音转文字未启用，请在设置中开启")
	}
	b := a.sttBridgeFor()
	if err := b.Start(); err != nil {
		return err
	}
	return b.StartListening()
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
	}
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
	return lang
}
