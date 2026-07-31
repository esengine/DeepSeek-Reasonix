package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"regexp"
	"runtime"
	"strings"
	"time"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"reasonix/desktop/internal/update"
	"reasonix/internal/repair"
)

// updater_app.go is the auto-updater's bound command surface — the App methods the
// frontend calls — mirroring settings_app.go's "one file per concern" split. The
// transport-free logic lives in updater.go; this file is the Wails glue: it streams
// download progress as "updater:progress" events and routes macOS to the manual
// download path unless the macOS build was Developer ID signed and notarized.

var errUpdateManualRequired = errors.New("update: manual update required")
var errUpdateInProgress = errors.New("update: another download or install is already in progress")

var updaterRequestIDRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)

func validateUpdaterRequest(requestID, selectedChannel, expectedVersion string) (string, string, string, error) {
	requestID = strings.TrimSpace(requestID)
	if !updaterRequestIDRE.MatchString(requestID) {
		return "", "", "", fmt.Errorf("update: invalid request id")
	}
	selectedChannel = targetUpdateChannel(selectedChannel)
	expectedVersion = strings.TrimSpace(expectedVersion)
	var valid bool
	if selectedChannel == "preview" {
		valid = previewDesktopVersionRE.MatchString(expectedVersion)
	} else {
		valid = stableDesktopVersionRE.MatchString(expectedVersion)
	}
	if !valid {
		return "", "", "", fmt.Errorf("update: invalid %s version %q", selectedChannel, expectedVersion)
	}
	return requestID, selectedChannel, expectedVersion, nil
}

func (a *App) beginUpdaterOperation(requestID string) (func(), error) {
	a.updaterOperationMu.Lock()
	defer a.updaterOperationMu.Unlock()
	if a.updaterOperationID != "" {
		return nil, errUpdateInProgress
	}
	a.updaterOperationID = requestID
	return func() {
		a.updaterOperationMu.Lock()
		if a.updaterOperationID == requestID {
			a.updaterOperationID = ""
		}
		a.updaterOperationMu.Unlock()
	}, nil
}

func ensureExpectedUpdateVersion(selectedChannel, expectedVersion, actualVersion string) error {
	if actualVersion == expectedVersion {
		return nil
	}
	return fmt.Errorf(
		"update: %s pointer changed from %s to %s; check again before downloading",
		selectedChannel,
		expectedVersion,
		actualVersion,
	)
}

// Version returns the build version injected via -ldflags (see main.go). The
// frontend displays it; CheckUpdate compares against it.
func (a *App) Version() string { return version }

// CheckUpdate fetches the manifest (R2, then GitHub) and reports whether a newer
// build is available for this platform. Safe to call on startup: a network error
// surfaces in UpdateInfo.Err rather than failing, so the UI can stay quiet.
func (a *App) CheckUpdate(selectedChannel string) (*UpdateInfo, error) {
	selectedChannel = targetUpdateChannel(selectedChannel)
	profile := detectInstallProfile()
	c, err := httpClient()
	if err != nil {
		a.recordUpdateError(err)
		return &UpdateInfo{
			Current:           version,
			Channel:           selectedChannel,
			CanSelfUpdate:     profile.CanSelfUpdate && canSelfUpdate(),
			ManualOnly:        !(profile.CanSelfUpdate && canSelfUpdate()),
			ManualReason:      firstNonEmptyStr(profile.ManualReason, manualUpdateReason()),
			InstallMode:       profile.Mode,
			RequiresElevation: profile.RequiresElev,
			DownloadURL:       downloadPage(selectedChannel),
			Err:               err.Error(),
		}, nil
	}
	ctx, cancel := context.WithTimeout(a.reqCtx(), httpTimeout)
	defer cancel()
	v4, _ := httpClientIPv4()
	m, err := fetchManifest(ctx, c, v4, selectedChannel)
	if err != nil {
		a.recordUpdateError(err)
		return &UpdateInfo{
			Current:           version,
			Channel:           selectedChannel,
			CanSelfUpdate:     profile.CanSelfUpdate && canSelfUpdate(),
			ManualOnly:        !(profile.CanSelfUpdate && canSelfUpdate()),
			ManualReason:      firstNonEmptyStr(profile.ManualReason, manualUpdateReason()),
			InstallMode:       profile.Mode,
			RequiresElevation: profile.RequiresElev,
			DownloadURL:       downloadPage(selectedChannel),
			Err:               err.Error(),
		}, nil
	}
	info := evaluateForChannel(version, selectedChannel, m)
	return &info, nil
}

// OpenDownloadPage opens the install page in the browser — the macOS manual-update
// path and a fallback link elsewhere.
func (a *App) OpenDownloadPage() {
	a.openDownloadPage(targetUpdateChannel(""))
}

func (a *App) openDownloadPage(selectedChannel string) {
	selectedChannel = targetUpdateChannel(selectedChannel)
	page := downloadPage(selectedChannel)
	if c, err := httpClient(); err == nil {
		ctx, cancel := context.WithTimeout(a.reqCtx(), httpTimeout)
		defer cancel()
		v4, _ := httpClientIPv4()
		if m, err := fetchManifest(ctx, c, v4, selectedChannel); err == nil {
			page = manifestDownloadPage(selectedChannel, m.DownloadPage)
		}
	}
	if a.ctx != nil {
		wruntime.BrowserOpenURL(a.ctx, page)
	}
}

// DownloadUpdate preserves the original one-argument Wails contract. It derives
// the version and request identity from the selected channel before entering the
// request-bound implementation used by the current frontend.
func (a *App) DownloadUpdate(selectedChannel string) (*UpdateDownloadResult, error) {
	selectedChannel = targetUpdateChannel(selectedChannel)
	info, err := a.CheckUpdate(selectedChannel)
	if err != nil {
		return nil, err
	}
	if info == nil || info.Err != "" || !info.Available {
		return nil, fmt.Errorf("update: no checked %s update is available", selectedChannel)
	}
	requestID := fmt.Sprintf("legacy-download-%d", time.Now().UnixNano())
	return a.DownloadUpdateRequest(selectedChannel, info.Latest, requestID)
}

// DownloadUpdateRequest downloads, verifies, and caches the exact version bound
// to a frontend request. Installation remains a separate explicit user action.
func (a *App) DownloadUpdateRequest(selectedChannel, expectedVersion, requestID string) (*UpdateDownloadResult, error) {
	requestID, selectedChannel, expectedVersion, err := validateUpdaterRequest(requestID, selectedChannel, expectedVersion)
	if err != nil {
		return nil, err
	}
	finish, err := a.beginUpdaterOperation(requestID)
	if err != nil {
		return nil, err
	}
	defer finish()

	profile := detectInstallProfile()
	if !profile.CanSelfUpdate || !canSelfUpdate() {
		return nil, a.requireManualUpdate(requestID, selectedChannel, expectedVersion, profile)
	}
	c, err := httpClient()
	if err != nil {
		return nil, a.failUpdate(requestID, selectedChannel, expectedVersion, err)
	}
	ctx, cancel := context.WithTimeout(a.reqCtx(), httpTimeout)
	defer cancel()
	v4, _ := httpClientIPv4()
	m, err := fetchManifest(ctx, c, v4, selectedChannel)
	if err != nil {
		return nil, a.failUpdate(requestID, selectedChannel, expectedVersion, err)
	}
	if err := ensureExpectedUpdateVersion(selectedChannel, expectedVersion, m.Version); err != nil {
		return nil, a.failUpdate(requestID, selectedChannel, expectedVersion, err)
	}
	profile = profileForManifest(profile, m)
	if !profile.CanSelfUpdate {
		return nil, a.requireManualUpdate(requestID, selectedChannel, expectedVersion, profile)
	}
	asset, kind, ok := selectUpdateAsset(m, profile)
	if !ok {
		return nil, a.failUpdate(requestID, selectedChannel, expectedVersion, fmt.Errorf("no update artifact for %s", update.CurrentPlatform()))
	}

	data, sig, err := a.downloadVerify(requestID, selectedChannel, expectedVersion, asset)
	if err != nil {
		return nil, a.failUpdate(requestID, selectedChannel, expectedVersion, err)
	}
	meta, err := saveCachedUpdateForChannel(selectedChannel, m.Version, asset, data, kind, sig)
	if err != nil {
		return nil, a.failUpdate(requestID, selectedChannel, expectedVersion, err)
	}
	a.emitProgress(requestID, selectedChannel, meta.Version, "downloaded", meta.Size, meta.Size, "")
	return &UpdateDownloadResult{
		RequestID: requestID,
		Version:   meta.Version,
		Channel:   meta.Channel,
		Path:      meta.Path,
		Size:      meta.Size,
		SHA256:    meta.SHA256,
	}, nil
}

// InstallUpdate preserves the original one-argument Wails contract. The cached
// artifact supplies the trusted expected version for the strict implementation.
func (a *App) InstallUpdate(selectedChannel string) error {
	selectedChannel = targetUpdateChannel(selectedChannel)
	meta, _, err := readVerifiedCachedUpdateForChannel(selectedChannel)
	if err != nil {
		return err
	}
	requestID := fmt.Sprintf("legacy-install-%d", time.Now().UnixNano())
	return a.InstallUpdateRequest(selectedChannel, meta.Version, requestID)
}

// InstallUpdateRequest applies the exact cached, verified update bound to a
// frontend request and then exits/relaunches.
func (a *App) InstallUpdateRequest(selectedChannel, expectedVersion, requestID string) error {
	requestID, selectedChannel, expectedVersion, err := validateUpdaterRequest(requestID, selectedChannel, expectedVersion)
	if err != nil {
		return err
	}
	finish, err := a.beginUpdaterOperation(requestID)
	if err != nil {
		return err
	}
	defer finish()

	profile := detectInstallProfile()
	if !profile.CanSelfUpdate || !canSelfUpdate() {
		return a.requireManualUpdate(requestID, selectedChannel, expectedVersion, profile)
	}
	meta, data, err := readVerifiedCachedUpdateForChannel(selectedChannel)
	if err != nil {
		return a.failUpdate(requestID, selectedChannel, expectedVersion, err)
	}
	if meta.Version != expectedVersion {
		return a.failUpdate(requestID, selectedChannel, expectedVersion, fmt.Errorf(
			"update: cached version %s does not match checked version %s",
			meta.Version,
			expectedVersion,
		))
	}
	// Re-detect install type at install time so a path change between download
	// and install cannot apply the wrong artifact kind.
	if c, err := httpClient(); err == nil {
		ctx, cancel := context.WithTimeout(a.reqCtx(), httpTimeout)
		defer cancel()
		v4, _ := httpClientIPv4()
		if m, err := fetchManifest(ctx, c, v4, selectedChannel); err == nil {
			profile = profileForManifest(detectInstallProfile(), m)
		} else {
			profile = detectInstallProfile()
		}
	} else {
		profile = detectInstallProfile()
	}
	if !profile.CanSelfUpdate {
		return a.requireManualUpdate(requestID, selectedChannel, expectedVersion, profile)
	}
	if err := ensureDebCacheMatchesProfile(meta, profile); err != nil {
		return a.failUpdate(requestID, selectedChannel, expectedVersion, err)
	}
	// Portable cache vs deb profile (and the reverse) are also rejected when
	// artifact kinds disagree with the active mode.
	wantKind := profile.ArtifactKind
	if wantKind == "" {
		wantKind = artifactKindTarball
	}
	if artifactKindFromMeta(meta.ArtifactKind) != artifactKindFromMeta(wantKind) {
		return a.failUpdate(requestID, selectedChannel, expectedVersion, errUpdateCacheMismatch)
	}

	switch profile.Mode {
	case installModeDeb:
		return a.installDebUpdate(requestID, meta)
	default:
		return a.installPortableUpdate(requestID, meta, data)
	}
}

func (a *App) installDebUpdate(requestID string, meta *cachedUpdate) error {
	// authorizing = Polkit password dialog. The helper streams
	// REASONIX_UPDATE_PHASE=installing on stderr after validation and before
	// apt-get, so the UI can leave authorizing while the package manager runs.
	a.emitProgress(requestID, meta.Channel, meta.Version, "authorizing", meta.Size, meta.Size, "")
	err := applyDebLinux(meta.Path, meta.SignaturePath, func(phase string) {
		if phase == "installing" {
			a.emitProgress(requestID, meta.Channel, meta.Version, "installing", meta.Size, meta.Size, "")
		}
	})
	if isAuthCancelled(err) {
		// User dismissed the Polkit dialog: keep the verified cache and return to
		// the downloaded state so they can retry. Do not count as an update error.
		a.recordUpdateEvent("authorization_cancelled")
		a.emitProgress(requestID, meta.Channel, meta.Version, "downloaded", meta.Size, meta.Size, "")
		return nil
	}
	if err != nil {
		if errors.Is(err, errUpdateAuthFailed) {
			// Surface a manual-install hint without writing /usr/bin ourselves.
			return a.failUpdate(requestID, meta.Channel, meta.Version, fmt.Errorf("%w. %s", err, manualDebInstallHint()))
		}
		return a.failUpdate(requestID, meta.Channel, meta.Version, err)
	}
	// Ensure installing was shown even if a phase line was missed (older helper).
	a.emitProgress(requestID, meta.Channel, meta.Version, "installing", meta.Size, meta.Size, "")
	a.emitProgress(requestID, meta.Channel, meta.Version, "done", meta.Size, meta.Size, "")
	a.shutdown(a.ctx)
	_ = relaunchThroughGuard()
	os.Exit(0)
	return nil
}

func (a *App) installPortableUpdate(requestID string, meta *cachedUpdate, data []byte) error {
	a.emitProgress(requestID, meta.Channel, meta.Version, "installing", meta.Size, meta.Size, "")
	var preparedUpdate *repair.UpdateTransaction
	if runtime.GOOS == "windows" || runtime.GOOS == "linux" {
		// Back up the complete release unit (main binary + Guard/launcher
		// siblings the installer also replaces) so rollback never leaves a
		// mixed-version install. Deb installs deliberately skip this — Guard
		// cannot rewrite /usr/bin and would corrupt dpkg state.
		var err error
		preparedUpdate, err = repair.PrepareFileUpdate(version, meta.Version, currentExecutablePath(), updateSiblingArtifacts()...)
		if err != nil {
			return a.failUpdate(requestID, meta.Channel, meta.Version, err)
		}
	}
	var err error
	switch runtime.GOOS {
	case "windows":
		err = applyWindowsFile(meta.Path, meta.SHA256, preparedUpdate)
	case "darwin":
		err = applyMac(meta.Path, meta.Version)
	case "linux":
		err = applyLinux(data, preparedUpdate)
	default:
		err = fmt.Errorf("self-update unsupported on %s", runtime.GOOS)
	}
	if err != nil {
		if runtime.GOOS == "linux" {
			// applyLinux replaces the Guard binary before the main-binary
			// swap, so a failure here can already have produced a mixed
			// install. Restore the recorded release unit instead of
			// discarding the rollback metadata; if the restore itself fails,
			// keep the pending transaction so Guard can retry the rollback on
			// the next launch.
			if preparedUpdate != nil {
				if _, rollbackErr := repair.RollbackPendingUpdateExact(preparedUpdate); rollbackErr != nil {
					err = errors.Join(err, fmt.Errorf("restore prepared release unit: %w", rollbackErr))
				} else if clearErr := repair.ClearUpdateApplyFailureExact(preparedUpdate); clearErr != nil {
					err = errors.Join(err, fmt.Errorf("clear update recovery marker: %w", clearErr))
				}
			}
		} else if runtime.GOOS == "windows" {
			// The helper may fail to start after another same-version attempt
			// has prepared a newer transaction. Cancel only this attempt.
			if preparedUpdate != nil {
				if cancelErr := repair.CancelPendingUpdateExact(preparedUpdate); cancelErr != nil {
					err = errors.Join(err, fmt.Errorf("cancel prepared update: %w", cancelErr))
				}
			}
		} else if runtime.GOOS != "darwin" {
			if preparedUpdate != nil {
				if cancelErr := repair.CancelPendingUpdateExact(preparedUpdate); cancelErr != nil {
					err = errors.Join(err, fmt.Errorf("cancel prepared update: %w", cancelErr))
				}
			}
		}
		return a.failUpdate(requestID, meta.Channel, meta.Version, err)
	}

	a.emitProgress(requestID, meta.Channel, meta.Version, "done", meta.Size, meta.Size, "")

	// Persist the conversation and stop subprocesses before handing off (same as
	// shutdown). On Linux the binary is now replaced, so relaunch it; on Windows and
	// macOS the installer/helper we launched takes over once we exit.
	a.shutdown(a.ctx)
	if runtime.GOOS == "linux" {
		_ = relaunchThroughGuard()
	}
	os.Exit(0)
	return nil
}

// ApplyUpdate is kept for older frontend bindings and tests. New UI code uses the
// explicit download → install split.
func (a *App) ApplyUpdate() error {
	selectedChannel := targetUpdateChannel("")
	info, err := a.CheckUpdate(selectedChannel)
	if err != nil {
		return err
	}
	if info == nil || info.Err != "" || !info.Available {
		return fmt.Errorf("update: no checked update is available")
	}
	requestID := fmt.Sprintf("legacy-%d", time.Now().UnixNano())
	if _, err := a.DownloadUpdateRequest(selectedChannel, info.Latest, requestID+"-download"); err != nil {
		return err
	}
	return a.InstallUpdateRequest(selectedChannel, info.Latest, requestID+"-install")
}

// downloadVerify downloads the asset (streaming progress), verifies its minisign
// signature against the embedded public key, then its sha256. It returns the
// verified bytes and the raw signature (needed for deb helper re-verification).
func (a *App) downloadVerify(requestID, selectedChannel, expectedVersion string, asset update.Asset) (data, sig []byte, err error) {
	c, err := httpClient()
	if err != nil {
		return nil, nil, err
	}
	v4, _ := httpClientIPv4() // best-effort IPv4 fallback; nil just means retries reuse c
	data, err = downloadForChannel(a.reqCtx(), c, v4, selectedChannel, asset.URL, asset.Size, func(rcv, total int64) {
		a.emitProgress(requestID, selectedChannel, expectedVersion, "downloading", rcv, total, "")
	})
	if err != nil {
		return nil, nil, err
	}
	a.emitProgress(requestID, selectedChannel, expectedVersion, "verifying", asset.Size, asset.Size, "")
	sig, err = fetchBytesFallbackForChannelSized(
		a.reqCtx(),
		c,
		v4,
		selectedChannel,
		asset.Sig,
		maxDesktopSignatureSize,
	)
	if err != nil {
		return nil, nil, err
	}
	if err := update.Verify(data, sig); err != nil {
		return nil, nil, err
	}
	if err := checkSHA256(data, asset.SHA256); err != nil {
		return nil, nil, err
	}
	return data, sig, nil
}

// reqCtx is the context for updater HTTP calls — the Wails context once startup has
// run, else Background (CheckUpdate may, in theory, be reached before startup).
func (a *App) reqCtx() context.Context {
	if a.ctx != nil {
		return a.ctx
	}
	return context.Background()
}

func (a *App) emitProgress(requestID, selectedChannel, expectedVersion, phase string, received, total int64, errMsg string) {
	if a.ctx == nil {
		return
	}
	wruntime.EventsEmit(a.ctx, "updater:progress", updateProgress{
		RequestID: requestID,
		Version:   expectedVersion,
		Channel:   normalizeUpdateChannel(selectedChannel),
		Phase:     phase, Received: received, Total: total, Err: errMsg,
	})
}

// failUpdate emits an error progress event and returns the error to the caller.
func (a *App) failUpdate(requestID, selectedChannel, expectedVersion string, err error) error {
	a.recordUpdateError(err)
	a.emitProgress(requestID, selectedChannel, expectedVersion, "error", 0, 0, err.Error())
	return err
}

// requireManualUpdate moves the frontend out of its busy state before opening
// the download page. Install mode and manifest availability are re-checked at
// each updater boundary, so either can legitimately change after the frontend
// started downloading or authorizing.
func (a *App) requireManualUpdate(requestID, selectedChannel, expectedVersion string, profile installProfile) error {
	err := a.failUpdate(requestID, selectedChannel, expectedVersion, manualUpdateRequiredError(profile))
	a.openDownloadPage(selectedChannel)
	return err
}

func manualUpdateRequiredError(profile installProfile) error {
	reason := firstNonEmptyStr(profile.ManualReason, manualUpdateReason(), "automatic update is unavailable for this install")
	return fmt.Errorf("%w: %s", errUpdateManualRequired, reason)
}

func (a *App) recordUpdateError(err error) {
	if err == nil || version == "dev" {
		return
	}
	if isAuthCancelled(err) {
		// Cancellation is an expected user action, not a failure rate signal.
		return
	}
	if m := a.metrics.Load(); m != nil {
		m.inc("updater_error", errorClass(err.Error()))
	}
}

// recordUpdateEvent records a non-failure updater signal (e.g. auth cancelled).
func (a *App) recordUpdateEvent(bucket string) {
	if version == "dev" {
		return
	}
	if m := a.metrics.Load(); m != nil {
		m.inc("updater_event", bucket)
	}
}

func firstNonEmptyStr(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
