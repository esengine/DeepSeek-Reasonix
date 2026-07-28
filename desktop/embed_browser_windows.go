//go:build windows

package main

import "errors"

func platformEmbedAvailable() bool {
	// WebView2 child HWND embed is planned; unavailable until implemented.
	return false
}

//nolint:unused // only called from darwin emitEmbedBrowserState
func platformEmbedEngineName() string {
	return "webview2"
}

func platformEmbedShow() error {
	return errors.New("内嵌浏览器暂未支持 Windows，请使用 macOS 构建")
}

func platformEmbedHide() {}

func platformEmbedDestroy() {}

func platformEmbedSetBounds(_ EmbedBrowserBounds) {}

func platformEmbedNavigate(_ string) error {
	return errors.New("内嵌浏览器暂未支持 Windows")
}

func platformEmbedReload() {}

func platformEmbedGoBack() {}

func platformEmbedGoForward() {}

func platformEmbedSetZoom(_ float64) {}

func platformEmbedSnapshotPNG() (string, error) {
	return "", errors.New("内嵌浏览器暂未支持 Windows")
}

func platformEmbedSetPickMode(_ bool, _, _ string) {}
