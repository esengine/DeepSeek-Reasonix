package main

import (
	"context"
	"fmt"
	"os"
	"runtime"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"reasonix/desktop/internal/update"
)

// updater_app.go is the auto-updater's bound command surface — the App methods the
// frontend calls — mirroring settings_app.go's "one file per concern" split. The
// transport-free logic lives in updater.go; this file is the Wails glue: it streams
// download progress as "updater:progress" events and routes macOS to the manual
// download path (an unsigned .app can't be swapped in place under Gatekeeper).

// Version returns the build version injected via -ldflags (see main.go). The
// frontend displays it; CheckUpdate compares against it.
func (a *App) Version() string { return version }

// CheckUpdate fetches the manifest (R2, then GitHub) and reports whether a newer
// build is available for this platform. Safe to call on startup: a network error
// surfaces in UpdateInfo.Err rather than failing, so the UI can stay quiet.
func (a *App) CheckUpdate() (*UpdateInfo, error) {
	c, err := httpClient()
	if err != nil {
		return &UpdateInfo{
			Current:       version,
			CanSelfUpdate: canSelfUpdate(),
			DownloadURL:   downloadPage(),
			Err:           err.Error(),
		}, nil
	}
	ctx, cancel := context.WithTimeout(a.reqCtx(), httpTimeout)
	defer cancel()
	m, err := fetchManifest(ctx, c)
	if err != nil {
		return &UpdateInfo{
			Current:       version,
			CanSelfUpdate: canSelfUpdate(),
			DownloadURL:   downloadPage(),
			Err:           err.Error(),
		}, nil
	}
	info := evaluate(version, m)
	return &info, nil
}

// OpenDownloadPage opens the releases page in the browser — the macOS manual-update
// path and a fallback link elsewhere.
func (a *App) OpenDownloadPage() {
	page := downloadPage()
	if c, err := httpClient(); err == nil {
		ctx, cancel := context.WithTimeout(a.reqCtx(), httpTimeout)
		defer cancel()
		if m, err := fetchManifest(ctx, c); err == nil && m.DownloadPage != "" {
			page = m.DownloadPage
		}
	}
	if a.ctx != nil {
		wruntime.BrowserOpenURL(a.ctx, page)
	}
}

// ApplyUpdate downloads, verifies, installs the latest build, then relaunches. On
// macOS it can't self-update (unsigned bundle), so it defers to the download page.
// Progress is streamed on the "updater:progress" event; on success the process exits.
func (a *App) ApplyUpdate() error {
	if !canSelfUpdate() {
		a.OpenDownloadPage()
		return nil
	}
	c, err := httpClient()
	if err != nil {
		return a.failUpdate(err)
	}
	ctx, cancel := context.WithTimeout(a.reqCtx(), httpTimeout)
	defer cancel()
	m, err := fetchManifest(ctx, c)
	if err != nil {
		return a.failUpdate(err)
	}
	asset, ok := m.Asset()
	if !ok {
		return a.failUpdate(fmt.Errorf("no update artifact for %s", update.CurrentPlatform()))
	}

	data, err := a.downloadVerify(asset)
	if err != nil {
		return a.failUpdate(err)
	}

	a.emitProgress("applying", asset.Size, asset.Size, "")
	switch runtime.GOOS {
	case "windows":
		err = applyWindows(data)
	case "linux":
		err = applyLinux(data)
	default:
		err = fmt.Errorf("self-update unsupported on %s", runtime.GOOS)
	}
	if err != nil {
		return a.failUpdate(err)
	}

	a.emitProgress("done", asset.Size, asset.Size, "")

	// Persist the conversation and stop subprocesses before handing off (same as
	// shutdown). On Linux the binary is now replaced, so relaunch it; on Windows the
	// installer we launched takes over once we exit.
	a.shutdown(a.ctx)
	if runtime.GOOS == "linux" {
		_ = relaunch()
	}
	os.Exit(0)
	return nil
}

// downloadVerify downloads the asset (streaming progress), verifies its minisign
// signature against the embedded public key, then its sha256. It returns the
// verified bytes and never touches disk on a bad signature.
func (a *App) downloadVerify(asset update.Asset) ([]byte, error) {
	c, err := httpClient()
	if err != nil {
		return nil, err
	}
	v4, _ := httpClientIPv4() // best-effort IPv4 fallback; nil just means retries reuse c
	data, err := download(a.reqCtx(), c, v4, asset.URL, asset.Size, func(rcv, total int64) {
		a.emitProgress("downloading", rcv, total, "")
	})
	if err != nil {
		return nil, err
	}
	a.emitProgress("verifying", asset.Size, asset.Size, "")
	sig, err := fetchBytes(a.reqCtx(), c, asset.Sig)
	if err != nil {
		return nil, err
	}
	if err := update.Verify(data, sig); err != nil {
		return nil, err
	}
	if err := checkSHA256(data, asset.SHA256); err != nil {
		return nil, err
	}
	return data, nil
}

// reqCtx is the context for updater HTTP calls — the Wails context once startup has
// run, else Background (CheckUpdate may, in theory, be reached before startup).
func (a *App) reqCtx() context.Context {
	if a.ctx != nil {
		return a.ctx
	}
	return context.Background()
}

func (a *App) emitProgress(phase string, received, total int64, errMsg string) {
	if a.ctx == nil {
		return
	}
	wruntime.EventsEmit(a.ctx, "updater:progress", updateProgress{
		Phase: phase, Received: received, Total: total, Err: errMsg,
	})
}

// failUpdate emits an error progress event and returns the error to the caller.
func (a *App) failUpdate(err error) error {
	a.emitProgress("error", 0, 0, err.Error())
	return err
}
