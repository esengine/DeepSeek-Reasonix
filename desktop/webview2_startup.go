package main

import (
	"context"
	goruntime "runtime"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

const windowsWebView2StartupFallbackDelay = 15 * time.Second

const windowsWebView2StartupFallbackMessage = "The desktop interface did not become ready within 15 seconds. " +
	"An unavailable Windows system proxy or a WebView2 failure may be blocking startup. " +
	"Check the system proxy, then restart Reasonix. If the problem continues, start Reasonix with --safe-mode.\n\n" +
	"桌面界面在 15 秒内未能就绪。不可用的 Windows 系统代理或 WebView2 故障可能阻塞了启动。" +
	"请检查系统代理后重启 Reasonix；如果问题仍然存在，请使用 --safe-mode 启动。"

// startWindowsWebView2StartupFallback prevents StartHidden from turning a slow
// or failed WebView2 navigation into an apparently missing application. Proxy
// isolation is the primary repair; this watchdog is the last-resort visible
// recovery path for policy-forced proxies and unrelated WebView2 failures.
func (a *App) startWindowsWebView2StartupFallback(ctx context.Context) {
	if !shouldStartWindowsWebView2StartupFallback(goruntime.GOOS) {
		return
	}
	go func() {
		timer := time.NewTimer(windowsWebView2StartupFallbackDelay)
		defer timer.Stop()
		if !awaitStartupFallback(ctx, timer.C, a.startupReady.Load) {
			return
		}

		// Show the native shell first so a dialog failure can never leave the app
		// completely invisible. The dark native background is already configured.
		runtime.WindowShow(ctx)
		if a.startupReady.Load() {
			return
		}
		_, _ = runtime.MessageDialog(ctx, runtime.MessageDialogOptions{
			Type:          runtime.WarningDialog,
			Title:         "Reasonix startup delayed / Reasonix 启动延迟",
			Message:       windowsWebView2StartupFallbackMessage,
			Buttons:       []string{"OK"},
			DefaultButton: "OK",
		})
	}()
}

func shouldStartWindowsWebView2StartupFallback(goos string) bool {
	return goos == "windows"
}

func awaitStartupFallback(ctx context.Context, timeout <-chan time.Time, ready func() bool) bool {
	select {
	case <-ctx.Done():
		return false
	case <-timeout:
		return !ready()
	}
}
