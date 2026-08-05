//go:build windows

package main

import (
	"sync"

	"github.com/wailsapp/go-webview2/webviewloader"
)

var (
	webView2RuntimeOnce   sync.Once
	webView2RuntimeCached string
)

func webView2RuntimeVersion() string {
	webView2RuntimeOnce.Do(func() {
		version, err := webviewloader.GetAvailableCoreWebView2BrowserVersionString("")
		if err == nil {
			webView2RuntimeCached = version
		}
	})
	return webView2RuntimeCached
}
