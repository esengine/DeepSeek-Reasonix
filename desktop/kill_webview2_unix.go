//go:build !windows

package main

// WebView2 only exists on Windows. macOS uses WKWebView (in-process),
// Linux uses WebKitGTK (in-process). No cleanup needed.
func killWebView2Processes() {}
func killOrphanWebView2() {}