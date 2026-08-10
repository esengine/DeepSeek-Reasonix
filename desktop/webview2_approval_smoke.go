package main

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	goruntime "runtime"
	"strings"
	"sync/atomic"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

const webView2ApprovalSmokeEnv = "REASONIX_WEBVIEW2_APPROVAL_SMOKE"

var webView2ApprovalSmokeScript = []byte(`<script>window.__REASONIX_WEBVIEW2_APPROVAL_SMOKE__=true;</script>`)

type webView2ApprovalSmokeState struct {
	enabled bool
	done    atomic.Bool
	passed  atomic.Bool
}

var activeWebView2ApprovalSmoke webView2ApprovalSmokeState

type WebView2ApprovalSmokeBridge struct {
	app *App
}

func webView2ApprovalSmokeRequested() bool {
	return webView2ApprovalSmokeRequestedFor(goruntime.GOOS, os.Getenv(webView2ApprovalSmokeEnv))
}

func webView2ApprovalSmokeRequestedFor(goos, value string) bool {
	return goos == "windows" && strings.TrimSpace(value) == "1"
}

func prepareWebView2ApprovalSmoke() bool {
	enabled := webView2ApprovalSmokeRequested()
	activeWebView2ApprovalSmoke.enabled = enabled
	if !enabled {
		capturePreviousFatalCrash()
		installFatalCrashOutput()
	}
	return enabled
}

func (a *App) webView2ApprovalSmokeLifecycle() (
	func(context.Context),
	func(context.Context),
	func(context.Context) bool,
	func(context.Context),
) {
	if !activeWebView2ApprovalSmoke.enabled {
		return a.startup, a.domReady, a.beforeClose, a.shutdown
	}
	return a.startWebView2ApprovalSmoke, a.domReadyWebView2ApprovalSmoke, nil, nil
}

func (a *App) startWebView2ApprovalSmoke(ctx context.Context) {
	a.ctx = ctx
	go func() {
		timer := time.NewTimer(45 * time.Second)
		defer timer.Stop()
		select {
		case <-timer.C:
			a.finishWebView2ApprovalSmoke(false, "timed out waiting for approval interaction")
		case <-ctx.Done():
		}
	}()
}

func (a *App) domReadyWebView2ApprovalSmoke(context.Context) {
	runtime.WindowCenter(a.ctx)
	runtime.WindowShow(a.ctx)
}

// Complete is inert outside the release smoke path. The production frontend
// never calls the bridge because the injected smoke marker is absent.
func (b *WebView2ApprovalSmokeBridge) Complete(ok bool, detail string) error {
	if !activeWebView2ApprovalSmoke.enabled || b.app == nil {
		return errors.New("WebView2 approval smoke is not enabled")
	}
	b.app.finishWebView2ApprovalSmoke(ok, detail)
	return nil
}

func (a *App) finishWebView2ApprovalSmoke(ok bool, detail string) {
	if !activeWebView2ApprovalSmoke.done.CompareAndSwap(false, true) {
		return
	}
	activeWebView2ApprovalSmoke.passed.Store(ok)
	if ok {
		slog.Info("WebView2 approval smoke passed", "detail", detail)
	} else {
		slog.Error("WebView2 approval smoke failed", "detail", detail)
	}
	runtime.Quit(a.ctx)
}

type smokeAssetResponse struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (w *smokeAssetResponse) Header() http.Header { return w.header }
func (w *smokeAssetResponse) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
}
func (w *smokeAssetResponse) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.body.Write(p)
}

func (a *App) webView2ApprovalSmokeMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if !activeWebView2ApprovalSmoke.enabled {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet || (r.URL.Path != "/" && r.URL.Path != "/index.html") {
				next.ServeHTTP(w, r)
				return
			}
			capture := &smokeAssetResponse{header: make(http.Header)}
			next.ServeHTTP(capture, r)
			body := capture.body.Bytes()
			if bytes.Contains(body, []byte("<head>")) {
				body = bytes.Replace(body, []byte("<head>"), append([]byte("<head>"), webView2ApprovalSmokeScript...), 1)
			}
			for key, values := range capture.header {
				for _, value := range values {
					w.Header().Add(key, value)
				}
			}
			w.Header().Del("Content-Length")
			status := capture.status
			if status == 0 {
				status = http.StatusOK
			}
			w.WriteHeader(status)
			_, _ = w.Write(body)
		})
	}
}

func finishWebView2ApprovalSmokeProcess(err error) {
	if activeWebView2ApprovalSmoke.enabled && (err != nil || !activeWebView2ApprovalSmoke.passed.Load()) {
		os.Exit(1)
	}
}
